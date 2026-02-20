package activities

import (
	"context"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/openai/openai-go/v3/option"
	"go.temporal.io/sdk/temporal"

	"github.com/ansg191/job-temporal/internal/llm"
)

// ResponseTextFormat is a Temporal-serializable representation of a JSON schema text format.
type ResponseTextFormat = llm.ResponseTextFormat

type AIRequest struct {
	Model        string                 `json:"model"`
	Input        []llm.Message          `json:"input"`
	Tools        []llm.ToolDefinition   `json:"tools,omitempty"`
	Temperature  *float64               `json:"temperature,omitempty"`
	Text         *ResponseTextFormat    `json:"text,omitempty"`
	Instructions string                 `json:"instructions,omitempty"`
	Conversation *llm.ConversationState `json:"conversation,omitempty"`
}

type ConversationRequest struct {
	Model string        `json:"model"`
	Items []llm.Message `json:"items,omitempty"`
}

type AIResponse struct {
	OutputText   string                 `json:"output_text"`
	ToolCalls    []llm.ToolCall         `json:"tool_calls,omitempty"`
	Conversation *llm.ConversationState `json:"conversation,omitempty"`
}

func GenerateTextFormat[T any](name string) *ResponseTextFormat {
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		panic(err)
	}
	return &ResponseTextFormat{Name: name, Schema: schema, Strict: true}
}

func CallAI(ctx context.Context, request AIRequest) (*AIResponse, error) {
	ref, err := llm.ParseModelRef(request.Model)
	if err != nil {
		return nil, temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("invalid model %q: %v", request.Model, err),
			"InvalidModelConfigError",
			err,
		)
	}

	backend, err := llm.NewBackend(ref)
	if err != nil {
		return nil, temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("failed to resolve backend for model %q: %v", request.Model, err),
			"InvalidModelConfigError",
			err,
		)
	}

	resp, err := backend.Generate(ctx, llm.Request{
		Model:        ref.Model,
		Messages:     request.Input,
		Tools:        request.Tools,
		Temperature:  request.Temperature,
		Text:         request.Text,
		Instructions: request.Instructions,
		Conversation: request.Conversation,
	})
	if err != nil {
		if llm.IsConfigError(err) {
			return nil, temporal.NewNonRetryableApplicationError(
				fmt.Sprintf("invalid conversation/backend state for model %q: %v", request.Model, err),
				"InvalidConversationStateError",
				err,
			)
		}
		return nil, err
	}

	return &AIResponse{
		OutputText:   resp.OutputText,
		ToolCalls:    resp.ToolCalls,
		Conversation: resp.Conversation,
	}, nil
}

func CreateConversation(ctx context.Context, request ConversationRequest) (*llm.ConversationState, error) {
	ref, err := llm.ParseModelRef(request.Model)
	if err != nil {
		return nil, temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("invalid model %q: %v", request.Model, err),
			"InvalidModelConfigError",
			err,
		)
	}

	backend, err := llm.NewBackend(ref)
	if err != nil {
		return nil, temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("failed to resolve backend for model %q: %v", request.Model, err),
			"InvalidModelConfigError",
			err,
		)
	}

	state, err := backend.CreateConversation(ctx, llm.ConversationRequest{Model: ref.Model, Items: request.Items})
	if err != nil {
		if llm.IsConfigError(err) {
			return nil, temporal.NewNonRetryableApplicationError(
				fmt.Sprintf("invalid conversation/backend state for model %q: %v", request.Model, err),
				"InvalidConversationStateError",
				err,
			)
		}
		return nil, err
	}
	return state, nil
}

func modelContextWindow(model string) (int64, bool) {
	return llm.OpenAIModelContextWindow(model)
}

func contextManagementOptions(model string) []option.RequestOption {
	return llm.OpenAIContextManagementOptions(model)
}

func classifyOpenAIError(err error) error {
	return llm.ClassifyOpenAIError(err)
}
