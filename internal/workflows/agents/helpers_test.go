package agents

import (
	"testing"

	"github.com/ansg191/job-temporal/internal/llm"
)

// TestUserMessageContinuationHasNonEmptyContent verifies that building a user
// message from the continuation constant produces a valid, non-empty message
// so the Anthropic backend never serialises an empty text block.
func TestUserMessageContinuationHasNonEmptyContent(t *testing.T) {
	t.Parallel()

	msg := userMessage(continuationMessage)
	if msg.Role != llm.RoleUser {
		t.Fatalf("expected RoleUser, got %q", msg.Role)
	}
	if len(msg.Content) == 0 {
		t.Fatal("continuation user message must have at least one content part")
	}
	for i, part := range msg.Content {
		if part.Type == llm.ContentTypeText && part.Text == "" {
			t.Fatalf("content part %d is an empty text block; this causes Anthropic 400 errors", i)
		}
	}
}
