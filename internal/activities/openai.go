package activities

import (
	"context"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
)

type OpenAIResponsesRequest struct {
	Model       string                                   `json:"model"`
	Messages    []openai.ChatCompletionMessageParamUnion `json:"messages"`
	Tools       []openai.ChatCompletionToolUnionParam    `json:"tools"`
	Temperature param.Opt[float64]                       `json:"temperature"`
}

func CallAI(ctx context.Context, request OpenAIResponsesRequest) (*openai.ChatCompletion, error) {
	client := openai.NewClient(option.WithMaxRetries(0))

	return client.Chat.Completions.New(
		ctx,
		openai.ChatCompletionNewParams{
			Messages:    request.Messages,
			Model:       request.Model,
			Tools:       request.Tools,
			Temperature: request.Temperature,
		},
	)
}
