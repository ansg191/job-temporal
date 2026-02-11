package activities

import (
	"testing"

	"github.com/openai/openai-go/v3"
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

func TestContextManagementOptions(t *testing.T) {
	t.Parallel()

	opts := contextManagementOptions(openai.ChatModelGPT5_2)
	if len(opts) == 0 {
		t.Fatalf("expected context management options for supported model")
	}

	opts = contextManagementOptions("unknown-model")
	if len(opts) != 0 {
		t.Fatalf("expected no context management options for unsupported model")
	}
}
