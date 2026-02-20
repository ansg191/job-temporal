package llm

import (
	"context"
)

type Backend interface {
	CreateConversation(ctx context.Context, req ConversationRequest) (*ConversationState, error)
	Generate(ctx context.Context, req Request) (*Response, error)
}

func NewBackend(ref ModelRef) (Backend, error) {
	switch ref.Backend {
	case BackendOpenAI:
		return &openAIBackend{}, nil
	case BackendAnthropic:
		return &anthropicBackend{}, nil
	default:
		return nil, NewConfigError("unsupported backend %q", ref.Backend)
	}
}
