package workflows

import (
	"strconv"

	"go.temporal.io/sdk/workflow"

	"github.com/ansg191/job-temporal/internal/github"
	"github.com/ansg191/job-temporal/internal/workflows/agents"
)

func AgentWorkflow(ctx workflow.Context, owner, repo string, input string) (string, error) {
	ghOpts := github.ClientOptions{Owner: owner, Repo: repo}

	var branchName string
	err := workflow.ExecuteChildWorkflow(
		ctx,
		agents.BranchNameAgent,
		agents.BranchNameAgentRequest{
			ClientOptions:  ghOpts,
			JobDescription: input,
			Purpose:        agents.BranchNameAgentPurposeResume,
		},
	).Get(ctx, &branchName)
	if err != nil {
		return "", err
	}

	var pr int
	err = workflow.ExecuteChildWorkflow(
		ctx,
		agents.ResumeBuilderWorkflow,
		agents.ResumeBuilderAgentRequest{
			ClientOptions: ghOpts,
			BranchName:    branchName,
			Job:           input,
		},
	).Get(ctx, &pr)
	if err != nil {
		return "", err
	}

	err = workflow.ExecuteChildWorkflow(
		ctx,
		agents.ReviewAgent,
		agents.ReviewAgentArgs{
			Repo:       ghOpts,
			Pr:         pr,
			BranchName: branchName,
		},
	).Get(ctx, &pr)
	if err != nil {
		return "", err
	}

	return strconv.Itoa(pr), nil
}
