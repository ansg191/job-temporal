package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"
)

type anthropicBackend struct{}

const (
	anthropicDefaultMaxTokens int64 = 16384

	anthropicCompactionBetaHeader   = "compact-2026-01-12"
	anthropicCompactionEditType     = "compact_20260112"
	anthropicCompactionTriggerValue = int64(150000)
)

func (b *anthropicBackend) CreateConversation(_ context.Context, req ConversationRequest) (*ConversationState, error) {
	return &ConversationState{
		Backend:    string(BackendAnthropic),
		Provider:   string(BackendAnthropic),
		Transcript: append([]Message(nil), req.Items...),
	}, nil
}

func (b *anthropicBackend) Generate(ctx context.Context, req Request) (*Response, error) {
	var state *ConversationState
	if req.Conversation != nil {
		cloned := req.Conversation.Clone()
		state = &cloned
	}
	if state == nil {
		state = &ConversationState{Backend: string(BackendAnthropic), Provider: string(BackendAnthropic)}
	}
	if state.Backend != "" && state.Backend != string(BackendAnthropic) {
		return nil, NewConfigError("anthropic backend cannot use conversation backend %q", state.Backend)
	}
	if state.Provider != "" && state.Provider != string(BackendAnthropic) {
		return nil, NewConfigError("anthropic backend cannot use conversation provider %q", state.Provider)
	}
	state.Backend = string(BackendAnthropic)
	state.Provider = string(BackendAnthropic)
	state.Transcript = append(state.Transcript, req.Messages...)

	var responseSchema map[string]any
	if req.Text != nil {
		schema, err := toSchemaMap(req.Text.Schema)
		if err != nil {
			return nil, err
		}
		responseSchema = schema
	}

	buildParams := func(stablePrefixCount int) (anthropic.MessageNewParams, error) {
		messages, systemBlocks, callErr := anthropicMessagesFromTranscript(state.Transcript, req.Instructions, stablePrefixCount)
		if callErr != nil {
			return anthropic.MessageNewParams{}, callErr
		}
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

	stablePrefixCount := len(state.Transcript) - len(req.Messages)
	if stablePrefixCount < 0 {
		stablePrefixCount = 0
	}

	client := anthropic.NewClient()

	var output strings.Builder
	toolCalls := make([]ToolCall, 0)
	err := withActivityHeartbeat(
		ctx,
		providerHeartbeatInterval,
		func() any {
			return map[string]any{
				"backend":  string(BackendAnthropic),
				"provider": string(BackendAnthropic),
				"model":    req.Model,
			}
		},
		func() error {
			for {
				params, callErr := buildParams(stablePrefixCount)
				if callErr != nil {
					return callErr
				}

				message, callErr := client.Messages.New(ctx, params, anthropicRequestOptions(req.Model)...)
				if callErr != nil {
					return callErr
				}
				if message == nil {
					return fmt.Errorf("anthropic returned nil message")
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

				if anthropicShouldContinueStopReason(message.StopReason) {
					if len(assistant.Content) == 0 && len(assistant.ToolCalls) == 0 {
						return fmt.Errorf("anthropic stop_reason %q returned without assistant content", message.StopReason)
					}
					stablePrefixCount = len(state.Transcript)
					continue
				}
				if anthropicTerminalStopReason(message.StopReason) {
					return nil
				}
				return fmt.Errorf("unsupported anthropic stop_reason %q", message.StopReason)
			}
		},
	)
	if err != nil {
		return nil, ClassifyAnthropicError(err)
	}

	return &Response{
		OutputText:   output.String(),
		ToolCalls:    toolCalls,
		Conversation: state,
	}, nil
}

func anthropicMaxTokens(model string) int64 {
	_ = model
	return anthropicDefaultMaxTokens
}

func anthropicShouldContinueStopReason(reason anthropic.StopReason) bool {
	return reason == anthropic.StopReasonMaxTokens || reason == anthropic.StopReasonPauseTurn
}

func anthropicTerminalStopReason(reason anthropic.StopReason) bool {
	switch reason {
	case anthropic.StopReasonEndTurn,
		anthropic.StopReasonStopSequence,
		anthropic.StopReasonToolUse,
		anthropic.StopReasonRefusal:
		return true
	default:
		return false
	}
}

func anthropicRequestOptions(model string) []option.RequestOption {
	if !anthropicSupportsCompaction(model) {
		return nil
	}

	return []option.RequestOption{
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
	}
}

func anthropicSupportsCompaction(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return strings.Contains(model, "opus-4-6") || strings.Contains(model, "sonnet-4-6")
}

func anthropicThinkingConfig(model string) (anthropic.ThinkingConfigParamUnion, bool) {
	if !anthropicSupportsAdaptiveThinking(model) {
		return anthropic.ThinkingConfigParamUnion{}, false
	}

	adaptive := anthropic.NewThinkingConfigAdaptiveParam()
	return anthropic.ThinkingConfigParamUnion{OfAdaptive: &adaptive}, true
}

func anthropicSupportsAdaptiveThinking(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return strings.Contains(model, "opus-4-6") || strings.Contains(model, "sonnet-4-6")
}

func anthropicMessagesFromTranscript(
	transcript []Message,
	instructions string,
	stablePrefixCount int,
) ([]anthropic.MessageParam, []anthropic.TextBlockParam, error) {
	messages := make([]anthropic.MessageParam, 0, len(transcript))
	systemBlocks := make([]anthropic.TextBlockParam, 0)
	lastStableMessageIdx := -1

	instructions = strings.TrimSpace(instructions)
	if instructions != "" {
		systemBlocks = append(systemBlocks, anthropic.TextBlockParam{Text: instructions})
	}

	for idx, msg := range transcript {
		switch msg.Role {
		case RoleSystem:
			text := strings.TrimSpace(msg.Text())
			if text != "" {
				systemBlocks = append(systemBlocks, anthropic.TextBlockParam{Text: text})
			}
		case RoleUser:
			blocks, err := anthropicContentBlocksFromParts(msg.Content)
			if err != nil {
				return nil, nil, err
			}
			if len(blocks) == 0 {
				blocks = append(blocks, anthropic.NewTextBlock(""))
			}
			messages = append(messages, anthropic.NewUserMessage(blocks...))
			if idx < stablePrefixCount {
				lastStableMessageIdx = len(messages) - 1
			}
		case RoleAssistant:
			blocks, err := anthropicContentBlocksFromParts(msg.Content)
			if err != nil {
				return nil, nil, err
			}
			for _, toolCall := range msg.ToolCalls {
				var args map[string]any
				if strings.TrimSpace(toolCall.Arguments) != "" {
					if err = json.Unmarshal([]byte(toolCall.Arguments), &args); err != nil {
						return nil, nil, fmt.Errorf("failed to unmarshal tool call arguments for %s: %w", toolCall.Name, err)
					}
				}
				blocks = append(blocks, anthropic.NewToolUseBlock(toolCall.CallID, args, toolCall.Name))
			}
			blocks = ensureAnthropicAssistantMessageEnding(blocks)
			if len(blocks) == 0 {
				continue
			}
			messages = append(messages, anthropic.NewAssistantMessage(blocks...))
			if idx < stablePrefixCount {
				lastStableMessageIdx = len(messages) - 1
			}
		case RoleTool:
			messages = append(messages, anthropic.NewUserMessage(
				anthropic.NewToolResultBlock(msg.ToolCallID, msg.Text(), false),
			))
			if idx < stablePrefixCount {
				lastStableMessageIdx = len(messages) - 1
			}
		default:
			return nil, nil, fmt.Errorf("unsupported anthropic message role %q", msg.Role)
		}
	}
	if len(systemBlocks) > 0 {
		// Cache the stable system-prefix for repeated tool loops.
		systemBlocks[len(systemBlocks)-1].CacheControl = anthropic.NewCacheControlEphemeralParam()
	}
	if lastStableMessageIdx >= 0 {
		setAnthropicCacheControlOnLastContentBlock(&messages[lastStableMessageIdx])
	}

	return messages, systemBlocks, nil
}

func anthropicContentBlocksFromParts(parts []ContentPart) ([]anthropic.ContentBlockParamUnion, error) {
	blocks := make([]anthropic.ContentBlockParamUnion, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case ContentTypeText:
			blocks = append(blocks, anthropic.NewTextBlock(part.Text))
		case ContentTypeImageURL:
			blocks = append(blocks, anthropic.NewImageBlock(anthropic.URLImageSourceParam{URL: part.ImageURL}))
		case ContentTypeThinking:
			if strings.TrimSpace(part.Signature) == "" {
				return nil, fmt.Errorf("anthropic thinking content block missing signature")
			}
			blocks = append(blocks, anthropic.NewThinkingBlock(part.Signature, part.Thinking))
		case ContentTypeRedactedThinking:
			if strings.TrimSpace(part.Data) == "" {
				return nil, fmt.Errorf("anthropic redacted thinking content block missing data")
			}
			blocks = append(blocks, anthropic.NewRedactedThinkingBlock(part.Data))
		default:
			return nil, fmt.Errorf("unsupported anthropic content type %q", part.Type)
		}
	}
	return blocks, nil
}

func ensureAnthropicAssistantMessageEnding(blocks []anthropic.ContentBlockParamUnion) []anthropic.ContentBlockParamUnion {
	if len(blocks) == 0 {
		return blocks
	}
	last := blocks[len(blocks)-1]
	if last.OfThinking == nil && last.OfRedactedThinking == nil {
		return blocks
	}
	// Anthropic rejects assistant messages that end with thinking blocks.
	return append(blocks, anthropic.NewTextBlock(""))
}

func anthropicToolsFromCanonical(tools []ToolDefinition) []anthropic.ToolUnionParam {
	ret := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, tool := range tools {
		schema := anthropicToolSchema(tool.Parameters)
		toolParam := anthropic.ToolParam{
			Name:        tool.Name,
			InputSchema: schema,
		}
		if tool.Description != "" {
			toolParam.Description = anthropic.String(tool.Description)
		}
		if tool.Strict {
			toolParam.Strict = anthropic.Bool(true)
		}
		ret = append(ret, anthropic.ToolUnionParam{OfTool: &toolParam})
	}
	if len(ret) > 0 && ret[len(ret)-1].OfTool != nil {
		// Cache the full tool list by marking the final tool definition.
		ret[len(ret)-1].OfTool.CacheControl = anthropic.NewCacheControlEphemeralParam()
	}
	return ret
}

func setAnthropicCacheControlOnLastContentBlock(msg *anthropic.MessageParam) {
	if msg == nil || len(msg.Content) == 0 {
		return
	}
	cacheControl := anthropic.NewCacheControlEphemeralParam()
	for i := len(msg.Content) - 1; i >= 0; i-- {
		block := &msg.Content[i]
		switch {
		case block.OfText != nil:
			if block.OfText.Text == "" {
				continue
			}
			block.OfText.CacheControl = cacheControl
			return
		case block.OfImage != nil:
			block.OfImage.CacheControl = cacheControl
			return
		case block.OfDocument != nil:
			block.OfDocument.CacheControl = cacheControl
			return
		case block.OfToolResult != nil:
			block.OfToolResult.CacheControl = cacheControl
			return
		case block.OfToolUse != nil:
			block.OfToolUse.CacheControl = cacheControl
			return
		}
	}
}

func anthropicToolSchema(parameters map[string]any) anthropic.ToolInputSchemaParam {
	schema := anthropic.ToolInputSchemaParam{
		Type: constant.Object("object"),
	}
	if len(parameters) == 0 {
		return schema
	}
	schema.Properties = parameters["properties"]
	schema.Required = toRequiredStrings(parameters["required"])
	extra := make(map[string]any)
	for key, value := range parameters {
		if key == "type" || key == "properties" || key == "required" {
			continue
		}
		extra[key] = value
	}
	if len(extra) > 0 {
		schema.ExtraFields = extra
	}
	return schema
}

func toRequiredStrings(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		ret := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok {
				ret = append(ret, s)
			}
		}
		return ret
	default:
		return nil
	}
}
