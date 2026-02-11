package activities

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/conversations"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"go.temporal.io/sdk/temporal"
)

// ResponseTextFormat is a Temporal-serializable representation of a JSON schema text format.
// OpenAI SDK union types don't serialize properly through Temporal, so we use this intermediate type.
type ResponseTextFormat struct {
	Name   string `json:"name"`
	Schema any    `json:"schema"`
	Strict bool   `json:"strict"`
}

type OpenAIResponsesRequest struct {
	Model          string                       `json:"model"`
	Input          responses.ResponseInputParam `json:"input"`
	Tools          []responses.ToolUnionParam   `json:"tools"`
	Temperature    param.Opt[float64]           `json:"temperature"`
	Text           *ResponseTextFormat          `json:"text,omitempty"`
	Instructions   string                       `json:"instructions,omitempty"`
	ConversationID string                       `json:"conversation_id,omitempty"`
}

type OpenAIConversationRequest struct {
	Items responses.ResponseInputParam `json:"items,omitempty"`
}

func GenerateTextFormat[T any](name string) *ResponseTextFormat {
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		panic(err)
	}

	return &ResponseTextFormat{
		Name:   name,
		Schema: schema,
		Strict: true,
	}
}

func CallAI(ctx context.Context, request OpenAIResponsesRequest) (*responses.Response, error) {
	client := openai.NewClient(option.WithMaxRetries(0))

	params := responses.ResponseNewParams{
		Input:       responses.ResponseNewParamsInputUnion{OfInputItemList: request.Input},
		Model:       request.Model,
		Tools:       request.Tools,
		Temperature: request.Temperature,
		Store:       openai.Bool(false),
	}
	if request.ConversationID != "" {
		params.Conversation = responses.ResponseNewParamsConversationUnion{
			OfConversationObject: &responses.ResponseConversationParam{
				ID: request.ConversationID,
			},
		}
		params.Truncation = responses.ResponseNewParamsTruncationAuto
	}

	if request.Instructions != "" {
		params.Instructions = openai.String(request.Instructions)
	}

	if request.Text != nil {
		schemaMap, err := toSchemaMap(request.Text.Schema)
		if err != nil {
			return nil, err
		}

		params.Text = responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigUnionParam{
				OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
					Name:   request.Text.Name,
					Schema: schemaMap,
					Strict: openai.Bool(request.Text.Strict),
				},
			},
		}
	}

	resp, err := client.Responses.New(ctx, params, contextManagementOptions(request.Model)...)
	if err != nil {
		return nil, classifyOpenAIError(err)
	}
	return resp, nil
}

func CreateConversation(ctx context.Context, request OpenAIConversationRequest) (string, error) {
	client := openai.NewClient(option.WithMaxRetries(0))
	params := conversations.ConversationNewParams{
		Items: request.Items,
	}

	conversation, err := client.Conversations.New(ctx, params)
	if err != nil {
		return "", classifyOpenAIError(err)
	}

	return conversation.ID, nil
}

func contextManagementOptions(model string) []option.RequestOption {
	contextWindow, ok := modelContextWindow(model)
	if !ok {
		return nil
	}

	threshold := contextWindow / 2
	return []option.RequestOption{
		option.WithJSONSet("context_management", []map[string]any{
			{
				"type":              "compaction",
				"compact_threshold": threshold,
			},
		}),
	}
}

func classifyOpenAIError(err error) error {
	var apiErr *openai.Error
	if errors.As(err, &apiErr) && apiErr.StatusCode == 400 && apiErr.Type == "invalid_request_error" {
		msg := "openai invalid request"
		if apiErr.Param != "" {
			msg += " (param: " + apiErr.Param + ")"
		}
		if apiErr.Message != "" {
			msg += ": " + apiErr.Message
		}
		return temporal.NewNonRetryableApplicationError(msg, "OpenAIInvalidRequestError", err)
	}
	return err
}

func modelContextWindow(model string) (int64, bool) {
	switch model {
	case openai.ChatModelGPT5_2,
		openai.ChatModelGPT5_2_2025_12_11,
		openai.ChatModelGPT5_2Pro,
		openai.ChatModelGPT5_2Pro2025_12_11:
		return 400_000, true
	case openai.ChatModelGPT5_2ChatLatest:
		return 128_000, true
	default:
		return 0, false
	}
}

// toSchemaMap converts an arbitrary schema value to map[string]any via a JSON
// marshal/unmarshal round-trip, which handles both already-typed maps and
// struct-based schema representations (e.g. *jsonschema.Schema).
func toSchemaMap(schema any) (map[string]any, error) {
	if m, ok := schema.(map[string]any); ok {
		return m, nil
	}

	data, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}

	var m map[string]any
	if err = json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}
