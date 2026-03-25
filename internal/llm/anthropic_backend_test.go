package llm

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

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

func TestAnthropicToolSchema_StripsUnsupportedIntegerConstraints(t *testing.T) {
	t.Parallel()

	schema := anthropicToolSchema(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"page_start": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"maximum":     10,
				"description": "start page",
			},
		},
		"required": []string{"page_start"},
	})

	props, ok := schema.Properties.(map[string]any)
	if !ok {
		t.Fatalf("expected properties map, got %T", schema.Properties)
	}
	pageStart, ok := props["page_start"].(map[string]any)
	if !ok {
		t.Fatalf("expected page_start schema map, got %T", props["page_start"])
	}
	if _, ok := pageStart["minimum"]; ok {
		t.Fatalf("expected minimum to be removed: %#v", pageStart)
	}
	if _, ok := pageStart["maximum"]; ok {
		t.Fatalf("expected maximum to be removed: %#v", pageStart)
	}
	description, _ := pageStart["description"].(string)
	if !strings.Contains(description, "start page") {
		t.Fatalf("expected original description to be preserved, got %q", description)
	}
	if !strings.Contains(description, "{maximum: 10, minimum: 1}") {
		t.Fatalf("expected removed constraints to be appended to description, got %q", description)
	}
}

func TestSanitizeAnthropicSchemaMap_RecursivelyStripsUnsupportedIntegerConstraints(t *testing.T) {
	t.Parallel()

	schema := map[string]any{
		"type": "object",
		"$defs": map[string]any{
			"Range": map[string]any{
				"type":        "integer",
				"minimum":     2,
				"description": "range",
			},
		},
		"properties": map[string]any{
			"count": map[string]any{
				"type":    "integer",
				"minimum": 1,
			},
			"values": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":    "integer",
					"minimum": 3,
				},
			},
			"choice": map[string]any{
				"anyOf": []any{
					map[string]any{
						"type":    "integer",
						"minimum": 4,
					},
					map[string]any{
						"oneOf": []any{
							map[string]any{
								"type":    "integer",
								"minimum": 5,
							},
						},
					},
				},
			},
		},
	}

	sanitized := sanitizeAnthropicSchemaMap(schema)
	assertNoUnsupportedIntegerConstraints(t, sanitized)

	defs, _ := sanitized["$defs"].(map[string]any)
	rangeDef, _ := defs["Range"].(map[string]any)
	rangeDesc, _ := rangeDef["description"].(string)
	if !strings.Contains(rangeDesc, "{minimum: 2}") {
		t.Fatalf("expected defs integer constraint summary in description, got %q", rangeDesc)
	}
}

func TestSanitizeAnthropicSchemaMap_SanitizesOutputSchema(t *testing.T) {
	t.Parallel()

	outputSchema, err := toSchemaMap(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"score": map[string]any{
				"type":        "integer",
				"minimum":     0,
				"description": "final score",
			},
		},
		"required": []string{"score"},
	})
	if err != nil {
		t.Fatalf("unexpected toSchemaMap error: %v", err)
	}

	sanitized := sanitizeAnthropicSchemaMap(outputSchema)
	props, _ := sanitized["properties"].(map[string]any)
	score, _ := props["score"].(map[string]any)
	if _, ok := score["minimum"]; ok {
		t.Fatalf("expected minimum removed from output schema integer property: %#v", score)
	}
	desc, _ := score["description"].(string)
	if !strings.Contains(desc, "final score") || !strings.Contains(desc, "{minimum: 0}") {
		t.Fatalf("expected output schema description to preserve constraints, got %q", desc)
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

func assertNoUnsupportedIntegerConstraints(t *testing.T, node any) {
	t.Helper()

	switch typed := node.(type) {
	case map[string]any:
		typeName, _ := typed["type"].(string)
		if typeName == "integer" {
			for _, key := range anthropicUnsupportedIntegerSchemaKeys {
				if _, ok := typed[key]; ok {
					t.Fatalf("unexpected unsupported integer key %q in %#v", key, typed)
				}
			}
		}
		for _, value := range typed {
			assertNoUnsupportedIntegerConstraints(t, value)
		}
	case []any:
		for _, value := range typed {
			assertNoUnsupportedIntegerConstraints(t, value)
		}
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

func TestClassifyAnthropicError_429WithoutRetryAfterIsRetryable(t *testing.T) {
	t.Parallel()

	in := &anthropic.Error{
		StatusCode: 429,
		Response:   &http.Response{Header: http.Header{}},
	}
	out := ClassifyAnthropicError(in)

	var appErr *temporal.ApplicationError
	if !errors.As(out, &appErr) {
		t.Fatalf("expected application error, got %T", out)
	}
	if appErr.NonRetryable() {
		t.Fatalf("expected retryable application error")
	}
	if appErr.Type() != "AnthropicRateLimitedError" {
		t.Fatalf("expected AnthropicRateLimitedError, got %q", appErr.Type())
	}
	if appErr.NextRetryDelay() != 0 {
		t.Fatalf("expected zero next retry delay when retry-after missing, got %s", appErr.NextRetryDelay())
	}
}

func TestClassifyAnthropicError_429WithRetryAfterSetsNextRetryDelay(t *testing.T) {
	t.Parallel()

	in := &anthropic.Error{
		StatusCode: 429,
		Response: &http.Response{
			Header: http.Header{
				"Retry-After": []string{"7"},
			},
		},
	}
	out := ClassifyAnthropicError(in)

	var appErr *temporal.ApplicationError
	if !errors.As(out, &appErr) {
		t.Fatalf("expected application error, got %T", out)
	}
	if appErr.NonRetryable() {
		t.Fatalf("expected retryable application error")
	}
	if appErr.Type() != "AnthropicRateLimitedError" {
		t.Fatalf("expected AnthropicRateLimitedError, got %q", appErr.Type())
	}
	if appErr.NextRetryDelay() != 7*time.Second {
		t.Fatalf("expected next retry delay 7s, got %s", appErr.NextRetryDelay())
	}
	if !errors.Is(out, in) {
		t.Fatalf("expected wrapped cause to include original error")
	}
}

func TestClassifyAnthropicError_429WithNilResponse(t *testing.T) {
	t.Parallel()

	in := &anthropic.Error{
		StatusCode: 429,
		Response:   nil,
	}

	out := ClassifyAnthropicError(in)

	var appErr *temporal.ApplicationError
	if !errors.As(out, &appErr) {
		t.Fatalf("expected application error, got %T", out)
	}
	if appErr.NonRetryable() {
		t.Fatalf("expected retryable application error")
	}
	if appErr.Type() != "AnthropicRateLimitedError" {
		t.Fatalf("expected AnthropicRateLimitedError, got %q", appErr.Type())
	}
	if appErr.NextRetryDelay() != 0 {
		t.Fatalf("expected zero next retry delay when response is nil, got %s", appErr.NextRetryDelay())
	}
	if !errors.Is(out, in) {
		t.Fatalf("expected wrapped cause to include original error")
	}
}

func TestParseRetryAfter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		after string
		want  time.Duration
	}{
		{name: "seconds", after: "11", want: 11 * time.Second},
		{name: "invalid", after: "not-a-number", want: 0},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := parseRetryAfter(tc.after); got != tc.want {
				t.Fatalf("parseRetryAfter(%q) = %s, want %s", tc.after, got, tc.want)
			}
		})
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

func TestAnthropicMessagesFromTranscript_TrailingThinkingBlockNotAugmented(t *testing.T) {
	t.Parallel()

	// A mid-conversation assistant message ending with a thinking block should not
	// have an empty text appended; the following user message makes it valid.
	transcript := []Message{
		{
			Role: RoleAssistant,
			Content: []ContentPart{
				TextPart("answer"),
				ThinkingPart("sig-123", "reasoning"),
			},
		},
		TextMessage(RoleUser, "Please Continue..."),
	}

	msgs, _, err := anthropicMessagesFromTranscript(transcript, "", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (assistant + user), got %d", len(msgs))
	}
	// The assistant message must contain exactly the original 2 blocks — no empty
	// terminal text should be silently injected (empty blocks cause Anthropic 400s).
	if len(msgs[0].Content) != 2 {
		t.Fatalf("expected 2 assistant content blocks (text + thinking), got %d", len(msgs[0].Content))
	}
	if msgs[0].Content[0].OfText == nil {
		t.Fatalf("expected first block to be text")
	}
	if msgs[0].Content[1].OfThinking == nil {
		t.Fatalf("expected second block to be thinking")
	}
	// The continuation user message must be non-empty so Anthropic accepts it.
	if msgs[1].Content[0].OfText == nil || msgs[1].Content[0].OfText.Text == "" {
		t.Fatalf("expected non-empty user continuation message")
	}
}

func TestAnthropicMessagesFromTranscript_ThinkingOnlyAssistantFollowedByContinuationUser(t *testing.T) {
	t.Parallel()

	// Regression test for issue #16: when the model pauses/hits max_tokens with only
	// thinking content, the continuation user message ("Please Continue...") must be
	// non-empty and the assistant message must NOT have an empty text block injected
	// (empty text blocks cause Anthropic to return 400 "text content blocks must be non-empty").
	transcript := []Message{
		TextMessage(RoleUser, "do something"),
		{
			Role:    RoleAssistant,
			Content: []ContentPart{ThinkingPart("sig-123", "reasoning")},
		},
		TextMessage(RoleUser, "Please Continue..."),
	}

	msgs, _, err := anthropicMessagesFromTranscript(transcript, "", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	// Assistant message: only the thinking block, no injected empty text.
	if len(msgs[1].Content) != 1 {
		t.Fatalf("expected 1 assistant content block (thinking only, no empty text), got %d", len(msgs[1].Content))
	}
	if msgs[1].Content[0].OfThinking == nil {
		t.Fatalf("expected thinking block")
	}
	// Continuation user message must be non-empty.
	if msgs[2].Content[0].OfText == nil || msgs[2].Content[0].OfText.Text == "" {
		t.Fatalf("expected non-empty continuation user message, got empty text block")
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
