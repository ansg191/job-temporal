package agents

import (
	"github.com/openai/openai-go/v3/responses"
	"go.temporal.io/sdk/workflow"

	"github.com/ansg191/job-temporal/internal/activities"
)

// userMessage builds a user-role input message for the OpenAI responses API.
func userMessage(text string) responses.ResponseInputItemUnionParam {
	msg := responses.ResponseInputItemParamOfMessage(text, responses.EasyInputMessageRoleUser)
	if msg.OfMessage != nil {
		msg.OfMessage.Type = responses.EasyInputMessageTypeMessage
	}
	return msg
}

// systemMessage builds a system-role input message for the OpenAI responses API.
func systemMessage(text string) responses.ResponseInputItemUnionParam {
	msg := responses.ResponseInputItemParamOfMessage(text, responses.EasyInputMessageRoleSystem)
	if msg.OfMessage != nil {
		msg.OfMessage.Type = responses.EasyInputMessageTypeMessage
	}
	return msg
}

// filterFunctionCalls keeps only function_call output items.
func filterFunctionCalls(output []responses.ResponseOutputItemUnion) []responses.ResponseOutputItemUnion {
	var calls []responses.ResponseOutputItemUnion
	for _, item := range output {
		if item.Type == "function_call" {
			calls = append(calls, item)
		}
	}
	return calls
}

// hasFunctionCalls reports whether any output item is a function_call.
func hasFunctionCalls(output []responses.ResponseOutputItemUnion) bool {
	for _, item := range output {
		if item.Type == "function_call" {
			return true
		}
	}
	return false
}

func createConversation(ctx workflow.Context, items responses.ResponseInputParam) (string, error) {
	var conversationID string
	err := workflow.ExecuteActivity(
		ctx,
		activities.CreateConversation,
		activities.OpenAIConversationRequest{Items: items},
	).Get(ctx, &conversationID)
	if err != nil {
		return "", err
	}
	return conversationID, nil
}
