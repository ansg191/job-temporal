package activities

import (
	"context"
	"encoding/json"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
)

// ResponseTextFormat is a Temporal-serializable representation of a JSON schema text format.
// OpenAI SDK union types don't serialize properly through Temporal, so we use this intermediate type.
type ResponseTextFormat struct {
	Name   string `json:"name"`
	Schema any    `json:"schema"`
	Strict bool   `json:"strict"`
}

type OpenAIResponsesRequest struct {
	Model        string                              `json:"model"`
	Input        responses.ResponseInputParam        `json:"input"`
	Tools        []responses.ToolUnionParam          `json:"tools"`
	Temperature  param.Opt[float64]                  `json:"temperature"`
	Text         *ResponseTextFormat                 `json:"text,omitempty"`
	Instructions string                              `json:"instructions,omitempty"`
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

	return client.Responses.New(ctx, params)
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
