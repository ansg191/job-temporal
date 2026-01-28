package workflows

import (
	"go.temporal.io/sdk/workflow"

	"github.com/ansg191/job-temporal/internal/github"
	"github.com/ansg191/job-temporal/internal/workflows/agents"
)

type JobWorkflowRequest struct {
	github.ClientOptions
	JobDesc string `json:"job_desc"`
}

func JobWorkflow(ctx workflow.Context, req JobWorkflowRequest) (string, error) {
	// Create new final branch to merge changes into
	var branchName string
	err := workflow.ExecuteChildWorkflow(
		ctx,
		agents.BranchNameAgent,
		agents.BranchNameAgentRequest{
			ClientOptions:  req.ClientOptions,
			JobDescription: req.JobDesc,
			Purpose:        agents.BranchNameAgentPurposeFinal,
		},
	).Get(ctx, &branchName)
	if err != nil {
		return "", err
	}

	// Start cover letter & resume builder workflows in parallel
	resumeFut := workflow.ExecuteChildWorkflow(
		ctx,
		BuilderWorkflow,
		BuilderWorkflowRequest{
			ClientOptions: req.ClientOptions,
			JobDesc:       req.JobDesc,
			TargetBranch:  branchName,
			Purpose:       "resume",
			Builder:       "typst",
		},
	)
	coverLetterFut := workflow.ExecuteChildWorkflow(
		ctx,
		BuilderWorkflow,
		BuilderWorkflowRequest{
			ClientOptions: req.ClientOptions,
			JobDesc:       req.JobDesc,
			TargetBranch:  branchName,
			Purpose:       "cover_letter",
			Builder:       "typst",
		},
	)

	// Wait on futures
	if err = resumeFut.Get(ctx, nil); err != nil {
		return "", err
	}
	if err = coverLetterFut.Get(ctx, nil); err != nil {
		return "", err
	}

	return branchName, nil
}
