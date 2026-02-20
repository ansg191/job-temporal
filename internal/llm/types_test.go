package llm

import "testing"

func TestConversationStateClone_DeepCopyTranscript(t *testing.T) {
	t.Parallel()

	orig := ConversationState{
		Backend:  string(BackendOpenAI),
		Provider: string(BackendOpenAI),
		Transcript: []Message{
			{
				Role:    RoleUser,
				Content: []ContentPart{TextPart("hello")},
			},
			{
				Role:      RoleAssistant,
				ToolCalls: []ToolCall{{CallID: "c1", Name: "tool", Arguments: `{"a":1}`}},
			},
		},
	}

	cloned := orig.Clone()

	cloned.Transcript[0].Content[0].Text = "changed"
	cloned.Transcript[1].ToolCalls[0].Arguments = `{"a":2}`

	if got := orig.Transcript[0].Content[0].Text; got != "hello" {
		t.Fatalf("original content mutated: %q", got)
	}
	if got := orig.Transcript[1].ToolCalls[0].Arguments; got != `{"a":1}` {
		t.Fatalf("original tool args mutated: %q", got)
	}
}
