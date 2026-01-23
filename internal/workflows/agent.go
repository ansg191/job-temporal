package workflows

import (
	"go.temporal.io/sdk/workflow"

	"github.com/ansg191/job-temporal/internal/workflows/agents"
)

func AgentWorkflow(ctx workflow.Context, owner, repo string, input string) (string, error) {
	var branchName string
	err := workflow.ExecuteChildWorkflow(ctx, agents.BranchNameAgent, owner, repo, input).Get(ctx, &branchName)
	if err != nil {
		return "", err
	}

	var message string
	err = workflow.ExecuteChildWorkflow(ctx, agents.ResumeBuilderWorkflow, owner, repo, branchName, input).Get(ctx, &message)
	if err != nil {
		return "", err
	}
	return message, nil
}
