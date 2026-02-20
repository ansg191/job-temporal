package tools

import (
	"github.com/ansg191/job-temporal/internal/llm"
	"go.temporal.io/sdk/workflow"
)

func GetToolResult(ctx workflow.Context, fut workflow.Future, call llm.ToolCall) (llm.Message, error) {
	var result string
	err := fut.Get(ctx, &result)
	if err != nil {
		return llm.Message{}, err
	}

	return llm.ToolResultMessage(call.CallID, call.Name, result), nil
}

// ToolDispatcher is an interface for dispatching LLM tool calls and managing their asynchronous execution.
type ToolDispatcher interface {
	// Dispatch dispatches a tool call and returns a future to wait on.
	//
	// If the tool is not supported, it returns (nil, error) where the error message
	// will be sent back to the AI as a tool result.
	// If the tool is dispatched successfully, it returns (future, nil).
	Dispatch(ctx workflow.Context, call llm.ToolCall) (workflow.Future, error)
}

// ProcessToolCalls handles the common pattern of dispatching tool calls in parallel
// and collecting their results.
func ProcessToolCalls(
	ctx workflow.Context,
	toolCalls []llm.ToolCall,
	dispatch ToolDispatcher,
) []llm.Message {
	futs := make([]workflow.Future, len(toolCalls))
	errs := make([]error, len(toolCalls))

	for i, call := range toolCalls {
		futs[i], errs[i] = dispatch.Dispatch(ctx, call)
	}

	var messages []llm.Message
	for i, fut := range futs {
		call := toolCalls[i]

		if fut == nil {
			if errs[i] != nil {
				messages = append(messages, llm.ToolResultMessage(call.CallID, call.Name, errs[i].Error()))
			}
			continue
		}

		res, err := GetToolResult(ctx, fut, call)
		if err != nil {
			messages = append(messages, llm.ToolResultMessage(call.CallID, call.Name, err.Error()))
			continue
		}
		messages = append(messages, res)
	}

	return messages
}
