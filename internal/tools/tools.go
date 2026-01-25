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

// ToolDispatcher dispatches a tool call and returns a future to wait on.
// If the tool is not supported, it returns (nil, error) where the error message
// will be sent back to the AI as a tool result.
// If the tool is dispatched successfully, it returns (future, nil).
type ToolDispatcher func(ctx workflow.Context, call openai.ChatCompletionMessageToolCallUnion) (workflow.Future, error)

// ProcessToolCalls handles the common pattern of dispatching tool calls in parallel
// and collecting their results.
func ProcessToolCalls(
	ctx workflow.Context,
	toolCalls []openai.ChatCompletionMessageToolCallUnion,
	dispatch ToolDispatcher,
) []openai.ChatCompletionMessageParamUnion {
	futs := make([]workflow.Future, len(toolCalls))
	errs := make([]error, len(toolCalls))

	for i, call := range toolCalls {
		futs[i], errs[i] = dispatch(ctx, call)
	}

	var messages []openai.ChatCompletionMessageParamUnion
	for i, fut := range futs {
		callID := toolCalls[i].ID

		if fut == nil {
			if errs[i] != nil {
				messages = append(messages, openai.ToolMessage(errs[i].Error(), callID))
			}
			continue
		}

		res, err := GetToolResult(ctx, fut, callID)
		if err != nil {
			messages = append(messages, openai.ToolMessage(err.Error(), callID))
			continue
		}
		messages = append(messages, res)
	}

	return messages
}
