package agents

import (
	"encoding/json"
	"log"

	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
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

// appendOutput converts response output items into input items and appends them.
func appendOutput(messages responses.ResponseInputParam, output []responses.ResponseOutputItemUnion) responses.ResponseInputParam {
	for _, item := range output {
		switch item.Type {
		case "message":
			msg := item.AsMessage()
			if len(msg.Content) == 0 {
				log.Printf("skipping output message with empty content (id=%s status=%s)", msg.ID, msg.Status)
				continue
			}
			contentParams := make([]responses.ResponseOutputMessageContentUnionParam, 0, len(msg.Content))
			for _, content := range msg.Content {
				contentParams = append(contentParams, param.Override[responses.ResponseOutputMessageContentUnionParam](json.RawMessage(content.RawJSON())))
			}
			status := msg.Status
			if status == "" {
				status = responses.ResponseOutputMessageStatusCompleted
			}
			messages = append(messages, responses.ResponseInputItemParamOfOutputMessage(contentParams, msg.ID, status))
		case "function_call":
			messages = append(messages, responses.ResponseInputItemParamOfFunctionCall(item.Arguments, item.CallID, item.Name))
		default:
			log.Printf("unexpected response output item type: %s", item.Type)
		}
	}
	return messages
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
