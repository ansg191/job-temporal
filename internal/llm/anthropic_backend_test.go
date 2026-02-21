package llm

import (
	"errors"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"go.temporal.io/sdk/temporal"
)

func TestAnthropicMaxTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		model string
		want  int64
	}{
		{model: "claude-opus-4-6", want: anthropicDefaultMaxTokens},
		{model: "claude-sonnet-4-6", want: anthropicDefaultMaxTokens},
		{model: "claude-sonnet-4-5", want: anthropicDefaultMaxTokens},
		{model: "claude-haiku-4-5", want: anthropicDefaultMaxTokens},
		{model: "claude-4-foo", want: anthropicDefaultMaxTokens},
		{model: "claude-3-5-sonnet", want: anthropicDefaultMaxTokens},
		{model: "", want: anthropicDefaultMaxTokens},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.model, func(t *testing.T) {
			t.Parallel()
			got := anthropicMaxTokens(tc.model)
			if got != tc.want {
				t.Fatalf("anthropicMaxTokens(%q) = %d, want %d", tc.model, got, tc.want)
			}
		})
	}
}

func TestAnthropicStopReasonHandling(t *testing.T) {
	t.Parallel()

	if !anthropicShouldContinueStopReason(anthropic.StopReasonMaxTokens) {
		t.Fatalf("expected max_tokens to continue")
	}
	if !anthropicShouldContinueStopReason(anthropic.StopReasonPauseTurn) {
		t.Fatalf("expected pause_turn to continue")
	}
	if anthropicShouldContinueStopReason(anthropic.StopReasonToolUse) {
		t.Fatalf("did not expect tool_use to continue")
	}
	if !anthropicTerminalStopReason(anthropic.StopReasonToolUse) {
		t.Fatalf("expected tool_use to be terminal")
	}
	if anthropicTerminalStopReason(anthropic.StopReasonMaxTokens) {
		t.Fatalf("did not expect max_tokens to be terminal")
	}
}

func TestAnthropicToolSchemaAlwaysIncludesObjectType(t *testing.T) {
	t.Parallel()

	schema := anthropicToolSchema(nil)
	if string(schema.Type) != "object" {
		t.Fatalf("expected input schema type object, got %q", schema.Type)
	}
}

func TestAnthropicToolsFromCanonical_CachesLastTool(t *testing.T) {
	t.Parallel()

	tools := []ToolDefinition{
		{Name: "one"},
		{Name: "two"},
	}
	got := anthropicToolsFromCanonical(tools)
	if len(got) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(got))
	}
	if got[0].OfTool == nil || got[1].OfTool == nil {
		t.Fatalf("expected tool variants")
	}
	if got[0].OfTool.CacheControl.Type != "" {
		t.Fatalf("expected no cache control on first tool")
	}
	if string(got[1].OfTool.CacheControl.Type) != "ephemeral" {
		t.Fatalf("expected cache control on last tool")
	}
}

func TestAnthropicMessagesFromTranscript_CachesStablePrefix(t *testing.T) {
	t.Parallel()

	transcript := []Message{
		TextMessage(RoleSystem, "stable system"),
		TextMessage(RoleUser, "stable user"),
		TextMessage(RoleUser, "new user"),
	}
	msgs, system, err := anthropicMessagesFromTranscript(transcript, "", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(system) == 0 || string(system[len(system)-1].CacheControl.Type) != "ephemeral" {
		t.Fatalf("expected system cache control")
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 anthropic messages, got %d", len(msgs))
	}
	lastStable := msgs[0]
	if len(lastStable.Content) == 0 || lastStable.Content[0].OfText == nil {
		t.Fatalf("expected text content in stable message")
	}
	if string(lastStable.Content[len(lastStable.Content)-1].OfText.CacheControl.Type) != "ephemeral" {
		t.Fatalf("expected stable prefix cache control on last content block")
	}
}

func TestClassifyAnthropicError_400IsNonRetryable(t *testing.T) {
	t.Parallel()

	in := &anthropic.Error{StatusCode: 400}
	out := ClassifyAnthropicError(in)

	var appErr *temporal.ApplicationError
	if !errors.As(out, &appErr) {
		t.Fatalf("expected application error, got %T", out)
	}
	if !appErr.NonRetryable() {
		t.Fatalf("expected non-retryable application error")
	}
	if appErr.Type() != "AnthropicInvalidRequestError" {
		t.Fatalf("expected AnthropicInvalidRequestError, got %q", appErr.Type())
	}
}

func TestAnthropicSupportsCompaction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		model string
		want  bool
	}{
		{model: "claude-opus-4-6", want: true},
		{model: "claude-sonnet-4-6", want: true},
		{model: "claude-sonnet-4-5", want: false},
		{model: "claude-haiku-4-5", want: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.model, func(t *testing.T) {
			t.Parallel()
			if got := anthropicSupportsCompaction(tc.model); got != tc.want {
				t.Fatalf("anthropicSupportsCompaction(%q) = %v, want %v", tc.model, got, tc.want)
			}
		})
	}
}

func TestAnthropicSupportsAdaptiveThinking(t *testing.T) {
	t.Parallel()

	tests := []struct {
		model string
		want  bool
	}{
		{model: "claude-opus-4-6", want: true},
		{model: "claude-sonnet-4-6", want: true},
		{model: "claude-sonnet-4-5", want: false},
		{model: "claude-haiku-4-5", want: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.model, func(t *testing.T) {
			t.Parallel()
			if got := anthropicSupportsAdaptiveThinking(tc.model); got != tc.want {
				t.Fatalf("anthropicSupportsAdaptiveThinking(%q) = %v, want %v", tc.model, got, tc.want)
			}
		})
	}
}

func TestAnthropicThinkingConfig(t *testing.T) {
	t.Parallel()

	config, ok := anthropicThinkingConfig("claude-sonnet-4-6")
	if !ok {
		t.Fatalf("expected adaptive thinking config for claude-sonnet-4-6")
	}
	if config.OfAdaptive == nil {
		t.Fatalf("expected adaptive thinking variant")
	}
	if got := config.GetType(); got == nil || *got != "adaptive" {
		t.Fatalf("expected adaptive thinking type, got %v", got)
	}

	if _, ok := anthropicThinkingConfig("claude-sonnet-4-5"); ok {
		t.Fatalf("did not expect thinking config for unsupported model")
	}
}

func TestAnthropicContentBlocksFromParts_Thinking(t *testing.T) {
	t.Parallel()

	parts := []ContentPart{
		ThinkingPart("sig-123", "reasoning"),
		RedactedThinkingPart("redacted-payload"),
	}
	blocks, err := anthropicContentBlocksFromParts(parts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0].OfThinking == nil {
		t.Fatalf("expected thinking block")
	}
	if blocks[0].OfThinking.Signature != "sig-123" {
		t.Fatalf("unexpected signature: %q", blocks[0].OfThinking.Signature)
	}
	if blocks[1].OfRedactedThinking == nil {
		t.Fatalf("expected redacted thinking block")
	}
	if blocks[1].OfRedactedThinking.Data != "redacted-payload" {
		t.Fatalf("unexpected redacted payload: %q", blocks[1].OfRedactedThinking.Data)
	}
}

func TestAnthropicContentBlocksFromParts_ThinkingValidation(t *testing.T) {
	t.Parallel()

	if _, err := anthropicContentBlocksFromParts([]ContentPart{{Type: ContentTypeThinking, Thinking: "x"}}); err == nil {
		t.Fatalf("expected error for thinking block without signature")
	}
	if _, err := anthropicContentBlocksFromParts([]ContentPart{{Type: ContentTypeRedactedThinking}}); err == nil {
		t.Fatalf("expected error for redacted thinking block without data")
	}
}

func TestAnthropicMessagesFromTranscript_PreservesThinkingWithToolUse(t *testing.T) {
	t.Parallel()

	transcript := []Message{
		{
			Role:    RoleAssistant,
			Content: []ContentPart{ThinkingPart("sig-123", "reasoning")},
			ToolCalls: []ToolCall{
				{
					CallID:    "call-1",
					Name:      "search",
					Arguments: `{"q":"hello"}`,
				},
			},
		},
	}

	msgs, _, err := anthropicMessagesFromTranscript(transcript, "", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if len(msgs[0].Content) != 2 {
		t.Fatalf("expected 2 assistant content blocks, got %d", len(msgs[0].Content))
	}
	if msgs[0].Content[0].OfThinking == nil {
		t.Fatalf("expected first block to be thinking")
	}
	if msgs[0].Content[1].OfToolUse == nil {
		t.Fatalf("expected second block to be tool_use")
	}
}

func TestAnthropicMessagesFromTranscript_ThinkingTextAndToolUseOrder(t *testing.T) {
	t.Parallel()

	transcript := []Message{
		{
			Role: RoleAssistant,
			Content: []ContentPart{
				ThinkingPart("sig-abc", "internal reasoning"),
				TextPart("Now let me read the resume.typ file for context on the formatting:"),
			},
			ToolCalls: []ToolCall{
				{
					CallID:    "toolu_013pyGtVqmcLCiawGqAeTPpy",
					Name:      "get_file_contents",
					Arguments: `{"owner":"ansg191","path":"resume.typ","ref":"refs/heads/resume-pal-bse-def","repo":"resume"}`,
				},
			},
		},
	}

	msgs, _, err := anthropicMessagesFromTranscript(transcript, "", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if len(msgs[0].Content) != 3 {
		t.Fatalf("expected 3 assistant content blocks, got %d", len(msgs[0].Content))
	}
	if msgs[0].Content[0].OfThinking == nil {
		t.Fatalf("expected block 0 to be thinking")
	}
	if msgs[0].Content[1].OfText == nil {
		t.Fatalf("expected block 1 to be text")
	}
	if msgs[0].Content[2].OfToolUse == nil {
		t.Fatalf("expected block 2 to be tool_use")
	}
}

func TestAnthropicMessagesFromTranscript_AppendsTerminalTextAfterTrailingThinking(t *testing.T) {
	t.Parallel()

	transcript := []Message{
		{
			Role: RoleAssistant,
			Content: []ContentPart{
				TextPart("answer"),
				ThinkingPart("sig-123", "reasoning"),
			},
		},
	}

	msgs, _, err := anthropicMessagesFromTranscript(transcript, "", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if len(msgs[0].Content) != 3 {
		t.Fatalf("expected trailing thinking block to be preserved with terminal text, got %d blocks", len(msgs[0].Content))
	}
	if msgs[0].Content[0].OfText == nil {
		t.Fatalf("expected first block to be text")
	}
	if msgs[0].Content[1].OfThinking == nil {
		t.Fatalf("expected second block to be thinking")
	}
	if msgs[0].Content[2].OfText == nil || msgs[0].Content[2].OfText.Text != "" {
		t.Fatalf("expected final terminal text block to be empty text")
	}
}

func TestAnthropicMessagesFromTranscript_PreservesThinkingOnlyAssistantTurn(t *testing.T) {
	t.Parallel()

	transcript := []Message{
		{
			Role:    RoleAssistant,
			Content: []ContentPart{ThinkingPart("sig-123", "reasoning")},
		},
	}

	msgs, _, err := anthropicMessagesFromTranscript(transcript, "", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected thinking-only assistant turn to be preserved, got %d messages", len(msgs))
	}
	if len(msgs[0].Content) != 2 {
		t.Fatalf("expected thinking + terminal empty text blocks, got %d", len(msgs[0].Content))
	}
	if msgs[0].Content[0].OfThinking == nil {
		t.Fatalf("expected first block to be thinking")
	}
	if msgs[0].Content[1].OfText == nil || msgs[0].Content[1].OfText.Text != "" {
		t.Fatalf("expected second block to be terminal empty text")
	}
}

func TestSetAnthropicCacheControlOnLastContentBlock_SkipsEmptyTerminalText(t *testing.T) {
	t.Parallel()

	msg := anthropic.NewAssistantMessage(
		anthropic.NewTextBlock("answer"),
		anthropic.NewTextBlock(""),
	)
	setAnthropicCacheControlOnLastContentBlock(&msg)

	if string(msg.Content[1].OfText.CacheControl.Type) != "" {
		t.Fatalf("expected empty terminal text to have no cache control")
	}
	if string(msg.Content[0].OfText.CacheControl.Type) != "ephemeral" {
		t.Fatalf("expected previous non-empty text to receive cache control")
	}
}

func TestSetAnthropicCacheControlOnLastContentBlock_NoEligibleBlock(t *testing.T) {
	t.Parallel()

	msg := anthropic.NewAssistantMessage(
		anthropic.NewThinkingBlock("sig-123", "reasoning"),
		anthropic.NewTextBlock(""),
	)
	setAnthropicCacheControlOnLastContentBlock(&msg)

	if string(msg.Content[1].OfText.CacheControl.Type) != "" {
		t.Fatalf("expected empty terminal text to have no cache control")
	}
}
