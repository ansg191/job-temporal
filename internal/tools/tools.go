package tools

import (
	"github.com/openai/openai-go/v3"
	"go.temporal.io/sdk/workflow"
)

func GetToolResult(ctx workflow.Context, fut workflow.Future, callID string) (openai.ChatCompletionMessageParamUnion, error) {
	var result string
	err := fut.Get(ctx, &result)
	if err != nil {
		return openai.ChatCompletionMessageParamUnion{}, err
	}

	return openai.ToolMessage[string](result, callID), nil
}
