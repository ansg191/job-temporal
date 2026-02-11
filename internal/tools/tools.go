package tools

import (
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"go.temporal.io/sdk/workflow"
)

func GetToolResult(ctx workflow.Context, fut workflow.Future, callID string) (responses.ResponseInputItemUnionParam, error) {
	var result string
	err := fut.Get(ctx, &result)
	if err != nil {
		return responses.ResponseInputItemUnionParam{}, err
	}

	return responses.ResponseInputItemUnionParam{
		OfFunctionCallOutput: &responses.ResponseInputItemFunctionCallOutputParam{
			CallID: callID,
			Output: responses.ResponseInputItemFunctionCallOutputOutputUnionParam{
				OfString: openai.String(result),
			},
		},
	}, nil
}

// ToolDispatcher is an interface for dispatching LLM tool calls and managing their asynchronous execution.
type ToolDispatcher interface {
	// Dispatch dispatches a tool call and returns a future to wait on.
	//
	// If the tool is not supported, it returns (nil, error) where the error message
	// will be sent back to the AI as a tool result.
	// If the tool is dispatched successfully, it returns (future, nil).
	Dispatch(ctx workflow.Context, call responses.ResponseOutputItemUnion) (workflow.Future, error)
}

// ProcessToolCalls handles the common pattern of dispatching tool calls in parallel
// and collecting their results.
func ProcessToolCalls(
	ctx workflow.Context,
	toolCalls []responses.ResponseOutputItemUnion,
	dispatch ToolDispatcher,
) []responses.ResponseInputItemUnionParam {
	futs := make([]workflow.Future, len(toolCalls))
	errs := make([]error, len(toolCalls))

	for i, call := range toolCalls {
		futs[i], errs[i] = dispatch.Dispatch(ctx, call)
	}

	var messages []responses.ResponseInputItemUnionParam
	for i, fut := range futs {
		callID := toolCalls[i].CallID

		if fut == nil {
			if errs[i] != nil {
				errMsg := errs[i].Error()
				messages = append(messages, responses.ResponseInputItemUnionParam{
					OfFunctionCallOutput: &responses.ResponseInputItemFunctionCallOutputParam{
						CallID: callID,
						Output: responses.ResponseInputItemFunctionCallOutputOutputUnionParam{
							OfString: openai.String(errMsg),
						},
					},
				})
			}
			continue
		}

		res, err := GetToolResult(ctx, fut, callID)
		if err != nil {
			errMsg := err.Error()
			messages = append(messages, responses.ResponseInputItemUnionParam{
				OfFunctionCallOutput: &responses.ResponseInputItemFunctionCallOutputParam{
					CallID: callID,
					Output: responses.ResponseInputItemFunctionCallOutputOutputUnionParam{
						OfString: openai.String(errMsg),
					},
				},
			})
			continue
		}
		messages = append(messages, res)
	}

	return messages
}
