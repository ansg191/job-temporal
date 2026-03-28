package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// claudeBackend uses OAuth tokens from a Claude Max/Pro subscription rather
// than a standard API key. Reads auth.json written by opencode-claude-auth.
type claudeBackend struct{}

const (
	claudeDefaultCLIVersion = "2.1.80"
	claudeDefaultAuthFile   = ".local/share/opencode/auth.json"

	// The claude-code-20250219 beta requires this system prompt prefix for
	// non-haiku models. Without it, sonnet/opus return a 400 "Error".
	// https://github.com/griffinmartin/opencode-claude-auth/blob/020833b/src/index.ts#L46-L47
	// https://github.com/griffinmartin/opencode-claude-auth/blob/020833b/src/index.ts#L205-L215
	claudeSystemIdentityPrefix = "You are Claude Code, Anthropic's official CLI for Claude."
)

// claudeBaseBetas are the beta feature flags required for OAuth-authenticated
// requests to the Anthropic API, matching the opencode-claude-auth plugin.
// https://github.com/griffinmartin/opencode-claude-auth/blob/020833b/src/model-config.ts#L15-L21
var claudeBaseBetas = []string{
	"claude-code-20250219",
	"oauth-2025-04-20",
	"interleaved-thinking-2025-05-14",
	"prompt-caching-scope-2026-01-05",
	"context-management-2025-06-27",
}

// claudeAuthEntry represents the "anthropic" key inside auth.json.
// https://github.com/griffinmartin/opencode-claude-auth/blob/020833b/src/credentials.ts#L120-L125
type claudeAuthEntry struct {
	Type    string `json:"type"`
	Access  string `json:"access"`
	Refresh string `json:"refresh"`
	Expires int64  `json:"expires"` // unix timestamp in milliseconds
}

// claudeAuthFile is the top-level structure of auth.json.
// https://github.com/griffinmartin/opencode-claude-auth/blob/020833b/src/credentials.ts#L108-L125
type claudeAuthFile struct {
	Anthropic claudeAuthEntry `json:"anthropic"`
}

func newClaudeBackend() (*claudeBackend, error) {
	return &claudeBackend{}, nil
}

func (b *claudeBackend) CreateConversation(_ context.Context, req ConversationRequest) (*ConversationState, error) {
	return &ConversationState{
		Backend:    string(BackendClaude),
		Provider:   string(BackendAnthropic),
		Transcript: append([]Message(nil), req.Items...),
	}, nil
}

func (b *claudeBackend) Generate(ctx context.Context, req Request) (*Response, error) {
	var state *ConversationState
	if req.Conversation != nil {
		cloned := req.Conversation.Clone()
		state = &cloned
	}
	if state == nil {
		state = &ConversationState{Backend: string(BackendClaude), Provider: string(BackendAnthropic)}
	}
	if state.Backend != "" && state.Backend != string(BackendClaude) {
		return nil, NewConfigError("claude backend cannot use conversation backend %q", state.Backend)
	}
	if state.Provider != "" && state.Provider != string(BackendAnthropic) {
		return nil, NewConfigError("claude backend cannot use conversation provider %q", state.Provider)
	}
	state.Backend = string(BackendClaude)
	state.Provider = string(BackendAnthropic)
	state.Transcript = append(state.Transcript, req.Messages...)

	var responseSchema map[string]any
	if req.Text != nil {
		schema, err := toSchemaMap(req.Text.Schema)
		if err != nil {
			return nil, err
		}
		responseSchema = sanitizeAnthropicSchemaMap(schema)
	}

	stablePrefixCount := len(state.Transcript) - len(req.Messages)
	if stablePrefixCount < 0 {
		stablePrefixCount = 0
	}

	// Read token fresh each call -- activity calls are short-lived so no cache needed.
	token, err := readClaudeAuthToken()
	if err != nil {
		return nil, fmt.Errorf("claude auth: %w", err)
	}

	buildParams := func() (anthropic.MessageNewParams, error) {
		messages, systemBlocks, callErr := anthropicMessagesFromTranscript(state.Transcript, req.Instructions, stablePrefixCount)
		if callErr != nil {
			return anthropic.MessageNewParams{}, callErr
		}
		systemBlocks = claudePrependIdentity(systemBlocks)
		params := anthropic.MessageNewParams{
			Model:     anthropic.Model(req.Model),
			MaxTokens: anthropicMaxTokens(req.Model),
			Messages:  messages,
			System:    systemBlocks,
			Tools:     anthropicToolsFromCanonical(req.Tools),
		}
		if req.Temperature != nil {
			params.Temperature = anthropic.Float(*req.Temperature)
		}
		if thinking, ok := anthropicThinkingConfig(req.Model); ok {
			params.Thinking = thinking
		}
		if req.Text != nil {
			params.OutputConfig = anthropic.OutputConfigParam{
				Format: anthropic.JSONOutputFormatParam{
					Schema: responseSchema,
				},
			}
		}
		return params, nil
	}

	client := anthropic.NewClient(claudeClientOptions(token)...)

	var output strings.Builder
	toolCalls := make([]ToolCall, 0)
	var (
		stopReason     anthropic.StopReason
		shouldContinue bool
	)
	err = withActivityHeartbeat(
		ctx,
		providerHeartbeatInterval,
		func() any {
			return map[string]any{
				"backend":  string(BackendClaude),
				"provider": string(BackendAnthropic),
				"model":    req.Model,
			}
		},
		func() error {
			params, callErr := buildParams()
			if callErr != nil {
				return callErr
			}

			message, callErr := client.Messages.New(ctx, params, claudeRequestOptions(req.Model)...)
			if callErr != nil {
				return callErr
			}
			if message == nil {
				return fmt.Errorf("claude backend: anthropic returned nil message")
			}

			stopReason = message.StopReason
			shouldContinue = anthropicShouldContinueStopReason(message.StopReason)
			if !shouldContinue && !anthropicTerminalStopReason(message.StopReason) {
				return fmt.Errorf("unsupported anthropic stop_reason %q", message.StopReason)
			}

			assistant := Message{Role: RoleAssistant}
			for _, block := range message.Content {
				switch variant := block.AsAny().(type) {
				case anthropic.TextBlock:
					output.WriteString(variant.Text)
					assistant.Content = append(assistant.Content, TextPart(variant.Text))
				case anthropic.ThinkingBlock:
					assistant.Content = append(assistant.Content, ThinkingPart(variant.Signature, variant.Thinking))
				case anthropic.RedactedThinkingBlock:
					assistant.Content = append(assistant.Content, RedactedThinkingPart(variant.Data))
				case anthropic.ToolUseBlock:
					b, marshalErr := json.Marshal(variant.Input)
					if marshalErr != nil {
						return marshalErr
					}
					call := ToolCall{CallID: variant.ID, Name: variant.Name, Arguments: string(b)}
					toolCalls = append(toolCalls, call)
					assistant.ToolCalls = append(assistant.ToolCalls, call)
				}
			}
			if len(assistant.Content) > 0 || len(assistant.ToolCalls) > 0 {
				state.Transcript = append(state.Transcript, assistant)
			}
			if shouldContinue && len(assistant.Content) == 0 && len(assistant.ToolCalls) == 0 {
				return fmt.Errorf("anthropic stop_reason %q returned without assistant content", message.StopReason)
			}
			return nil
		},
	)
	if err != nil {
		return nil, ClassifyAnthropicError(err)
	}

	return &Response{
		OutputText:     output.String(),
		ToolCalls:      toolCalls,
		Conversation:   state,
		StopReason:     string(stopReason),
		ShouldContinue: shouldContinue,
	}, nil
}

// claudeAuthFilePath returns the path to auth.json, checking the
// CLAUDE_AUTH_FILE env var first, then falling back to ~/.local/share/opencode/auth.json.
// https://github.com/griffinmartin/opencode-claude-auth/blob/020833b/src/credentials.ts#L97-L106
func claudeAuthFilePath() (string, error) {
	if p := os.Getenv("CLAUDE_AUTH_FILE"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine home directory: %w", err)
	}
	return filepath.Join(home, claudeDefaultAuthFile), nil
}

func readClaudeAuthToken() (string, error) {
	path, err := claudeAuthFilePath()
	if err != nil {
		return "", err
	}
	return readClaudeAuthTokenFromFile(path)
}

// readClaudeAuthTokenFromFile reads and validates the OAuth access token from
// the given path. Extracted from readClaudeAuthToken for testability.
func readClaudeAuthTokenFromFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("could not read auth file %s: %w", path, err)
	}

	var authFile claudeAuthFile
	if err = json.Unmarshal(data, &authFile); err != nil {
		return "", fmt.Errorf("could not parse auth file %s: %w", path, err)
	}

	entry := authFile.Anthropic
	if entry.Access == "" {
		return "", fmt.Errorf("no access token in auth file %s", path)
	}
	if entry.Expires > 0 && entry.Expires < time.Now().UnixMilli() {
		return "", fmt.Errorf("claude auth token expired at %s",
			time.UnixMilli(entry.Expires).Format(time.RFC3339))
	}
	return entry.Access, nil
}

// claudeClientOptions configures Bearer token auth instead of x-api-key.
// https://github.com/griffinmartin/opencode-claude-auth/blob/020833b/src/index.ts#L127-L132
func claudeClientOptions(token string) []option.RequestOption {
	return []option.RequestOption{
		option.WithAuthToken(token),
		option.WithHeaderDel("X-Api-Key"),
	}
}

// claudeRequestOptions injects Claude Code identity/beta headers per request.
// https://github.com/griffinmartin/opencode-claude-auth/blob/020833b/src/index.ts#L82-L140
func claudeRequestOptions(model string) []option.RequestOption {
	cliVersion := claudeDefaultCLIVersion
	if v := os.Getenv("ANTHROPIC_CLI_VERSION"); v != "" {
		cliVersion = v
	}

	betas := make([]string, len(claudeBaseBetas))
	copy(betas, claudeBaseBetas)

	// https://github.com/griffinmartin/opencode-claude-auth/blob/020833b/src/model-config.ts#L27-L28
	if strings.Contains(strings.ToLower(model), "4-6") {
		betas = append(betas, "effort-2025-11-24")
	}

	userAgent := fmt.Sprintf("claude-cli/%s (external, cli)", cliVersion)
	if ua := os.Getenv("ANTHROPIC_USER_AGENT"); ua != "" {
		userAgent = ua
	}

	// https://github.com/griffinmartin/opencode-claude-auth/blob/020833b/src/index.ts#L137-L140
	billing := fmt.Sprintf("cc_version=%s.%s; cc_entrypoint=cli; cch=00000;", cliVersion, model)

	opts := []option.RequestOption{
		option.WithHeader("anthropic-beta", strings.Join(betas, ",")),
		option.WithHeader("x-app", "cli"),
		option.WithHeader("user-agent", userAgent),
		option.WithHeader("x-anthropic-billing-header", billing),
	}

	if anthropicSupportsCompaction(model) {
		opts = append(opts,
			option.WithHeaderAdd("anthropic-beta", anthropicCompactionBetaHeader),
			option.WithJSONSet("context_management", map[string]any{
				"edits": []map[string]any{
					{
						"type": anthropicCompactionEditType,
						"trigger": map[string]any{
							"type":  "input_tokens",
							"value": anthropicCompactionTriggerValue,
						},
					},
				},
			}),
		)
	}

	return opts
}

func claudeCLIVersion() string {
	if v := os.Getenv("ANTHROPIC_CLI_VERSION"); v != "" {
		return v
	}
	return claudeDefaultCLIVersion
}

// claudePrependIdentity ensures the Claude Code system identity prefix is
// present. Required by the claude-code-20250219 beta for non-haiku models.
// https://github.com/griffinmartin/opencode-claude-auth/blob/020833b/src/index.ts#L205-L215
func claudePrependIdentity(blocks []anthropic.TextBlockParam) []anthropic.TextBlockParam {
	for _, b := range blocks {
		if strings.Contains(b.Text, claudeSystemIdentityPrefix) {
			return blocks
		}
	}
	return append([]anthropic.TextBlockParam{{Text: claudeSystemIdentityPrefix}}, blocks...)
}
