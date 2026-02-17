package agents

import (
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"go.temporal.io/sdk/workflow"

	"github.com/ansg191/job-temporal/internal/activities"
	"github.com/ansg191/job-temporal/internal/config"
)

func messageWithRole[T string | responses.ResponseInputMessageContentListParam](
	content T,
	role responses.EasyInputMessageRole,
) responses.ResponseInputItemUnionParam {
	msg := responses.ResponseInputItemParamOfMessage(content, role)
	if msg.OfMessage != nil {
		msg.OfMessage.Type = responses.EasyInputMessageTypeMessage
	}
	return msg
}

// userMessage builds a user-role input message for the OpenAI responses API.
func userMessage[T string | responses.ResponseInputMessageContentListParam](content T) responses.ResponseInputItemUnionParam {
	return messageWithRole(content, responses.EasyInputMessageRoleUser)
}

// systemMessage builds a system-role input message for the OpenAI responses API.
func systemMessage[T string | responses.ResponseInputMessageContentListParam](content T) responses.ResponseInputItemUnionParam {
	return messageWithRole(content, responses.EasyInputMessageRoleSystem)
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

func withCallAIActivityOptions(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 15 * time.Minute,
		HeartbeatTimeout:    15 * time.Second,
	})
}

// loadAgentConfig executes the GetAgentConfig activity and returns the agent configuration.
func loadAgentConfig(ctx workflow.Context, agentName string) (*config.AgentConfig, error) {
	configCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Second,
	})
	var agentCfg config.AgentConfig
	err := workflow.ExecuteActivity(configCtx, config.GetAgentConfig, agentName).Get(ctx, &agentCfg)
	if err != nil {
		return nil, err
	}
	return &agentCfg, nil
}

// temperatureOpt converts a *float64 config temperature to the param.Opt type used by OpenAI.
func temperatureOpt(t *float64) (opt param.Opt[float64]) {
	if t != nil {
		opt = openai.Float(*t)
	}
	return
}
