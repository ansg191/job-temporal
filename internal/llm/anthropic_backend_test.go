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
