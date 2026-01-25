package activities

import (
	"context"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/shared"
)

// ResponseFormatSchema is a Temporal-serializable representation of a JSON schema response format.
// OpenAI SDK union types don't serialize properly through Temporal, so we use this intermediate type.
type ResponseFormatSchema struct {
	Name   string `json:"name"`
	Schema any    `json:"schema"`
	Strict bool   `json:"strict"`
}

type OpenAIResponsesRequest struct {
	Model          string                                   `json:"model"`
	Messages       []openai.ChatCompletionMessageParamUnion `json:"messages"`
	Tools          []openai.ChatCompletionToolUnionParam    `json:"tools"`
	Temperature    param.Opt[float64]                       `json:"temperature"`
	ResponseFormat *ResponseFormatSchema                    `json:"response_format,omitempty"`
}

func GenerateResponseFormat[T any](name string) *ResponseFormatSchema {
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		panic(err)
	}

	return &ResponseFormatSchema{
		Name:   name,
		Schema: schema,
		Strict: true,
	}
}

func CallAI(ctx context.Context, request OpenAIResponsesRequest) (*openai.ChatCompletion, error) {
	client := openai.NewClient(option.WithMaxRetries(0))

	params := openai.ChatCompletionNewParams{
		Messages:    request.Messages,
		Model:       request.Model,
		Tools:       request.Tools,
		Temperature: request.Temperature,
	}

	if request.ResponseFormat != nil {
		params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &shared.ResponseFormatJSONSchemaParam{
				JSONSchema: shared.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:   request.ResponseFormat.Name,
					Schema: request.ResponseFormat.Schema,
					Strict: openai.Bool(request.ResponseFormat.Strict),
				},
			},
		}
	}

	return client.Chat.Completions.New(ctx, params)
}
