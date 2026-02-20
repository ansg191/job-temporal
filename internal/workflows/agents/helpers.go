package agents

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/workflow"

	"github.com/ansg191/job-temporal/internal/activities"
	"github.com/ansg191/job-temporal/internal/config"
	"github.com/ansg191/job-temporal/internal/llm"
)

func userMessage(content string) llm.Message {
	return llm.TextMessage(llm.RoleUser, content)
}

func userMessageParts(parts []llm.ContentPart) llm.Message {
	return llm.Message{Role: llm.RoleUser, Content: parts}
}

func systemMessage(content string) llm.Message {
	return llm.TextMessage(llm.RoleSystem, content)
}

func hasFunctionCalls(calls []llm.ToolCall) bool {
	return len(calls) > 0
}

func createConversation(ctx workflow.Context, model string, items []llm.Message) (*llm.ConversationState, error) {
	var conversation llm.ConversationState
	err := workflow.ExecuteActivity(
		ctx,
		activities.CreateConversation,
		activities.ConversationRequest{Model: model, Items: items},
	).Get(ctx, &conversation)
	if err != nil {
		return nil, err
	}
	return &conversation, nil
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
	err := workflow.ExecuteActivity(configCtx, activities.GetAgentConfig, agentName).Get(ctx, &agentCfg)
	if err != nil {
		return nil, err
	}
	return &agentCfg, nil
}

func temperatureOpt(t *float64) *float64 {
	return t
}

func wrapLLMXML(tag, content string) string {
	return fmt.Sprintf("<%s>\n%s\n</%s>", tag, content, tag)
}
