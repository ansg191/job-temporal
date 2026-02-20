package llm

import "testing"

func TestParseModelRef_ValidFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		model    string
		backend  BackendType
		provider string
		id       string
	}{
		{
			name:     "openai",
			model:    "openai/gpt-5.2",
			backend:  BackendOpenAI,
			provider: "openai",
			id:       "gpt-5.2",
		},
		{
			name:     "anthropic",
			model:    "anthropic/sonnet-4.5",
			backend:  BackendAnthropic,
			provider: "anthropic",
			id:       "sonnet-4.5",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ref, err := ParseModelRef(tc.model)
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if ref.Backend != tc.backend {
				t.Fatalf("backend mismatch: want %q got %q", tc.backend, ref.Backend)
			}
			if ref.Provider != tc.provider {
				t.Fatalf("provider mismatch: want %q got %q", tc.provider, ref.Provider)
			}
			if ref.Model != tc.id {
				t.Fatalf("model id mismatch: want %q got %q", tc.id, ref.Model)
			}
		})
	}
}

func TestParseModelRef_InvalidFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		model string
	}{
		{name: "empty", model: ""},
		{name: "unprefixed", model: "gpt-5.2"},
		{name: "unknown-backend", model: "foo/gpt-5.2"},
		{name: "langchaingo-format-not-supported", model: "langchaingo/openai/gpt-5.2"},
		{name: "openai-empty-id", model: "openai/"},
		{name: "anthropic-empty-id", model: "anthropic/"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if _, err := ParseModelRef(tc.model); err == nil {
				t.Fatalf("expected error for model %q", tc.model)
			}
		})
	}
}
