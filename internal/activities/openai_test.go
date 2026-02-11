package activities

import (
	"encoding/json"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

func TestModelContextWindow(t *testing.T) {
	t.Parallel()

	window, ok := modelContextWindow(openai.ChatModelGPT5_2)
	if !ok {
		t.Fatalf("expected model to be supported")
	}
	if window != 400_000 {
		t.Fatalf("expected 400000, got %d", window)
	}

	_, ok = modelContextWindow("unknown-model")
	if ok {
		t.Fatalf("expected unknown model to be unsupported")
	}
}

func TestCompactedOutputToInput(t *testing.T) {
	t.Parallel()

	rawOutput := `[
		{
			"type":"message",
			"id":"msg_1",
			"role":"user",
			"status":"completed",
			"content":[{"type":"input_text","text":"hello"}]
		},
		{
			"type":"compaction",
			"id":"comp_1",
			"encrypted_content":"enc_123"
		}
	]`

	var output []responses.ResponseOutputItemUnion
	if err := json.Unmarshal([]byte(rawOutput), &output); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}

	input := compactedOutputToInput(output)
	if len(input) != 2 {
		t.Fatalf("expected 2 input items, got %d", len(input))
	}

	firstJSON, err := json.Marshal(input[0])
	if err != nil {
		t.Fatalf("marshal first item: %v", err)
	}
	if string(firstJSON) != `{"type":"message","id":"msg_1","role":"user","status":"completed","content":[{"type":"input_text","text":"hello"}]}` {
		t.Fatalf("unexpected first item json: %s", string(firstJSON))
	}

	secondJSON, err := json.Marshal(input[1])
	if err != nil {
		t.Fatalf("marshal second item: %v", err)
	}
	if string(secondJSON) != `{"type":"compaction","id":"comp_1","encrypted_content":"enc_123"}` {
		t.Fatalf("unexpected second item json: %s", string(secondJSON))
	}
}
