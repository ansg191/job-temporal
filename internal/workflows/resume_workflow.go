package workflows

import (
	"go.temporal.io/sdk/workflow"

	"github.com/ansg191/job-temporal/internal/github"
	"github.com/ansg191/job-temporal/internal/workflows/agents"
)

type ResumeWorkflowRequest struct {
	github.ClientOptions
	JobDesc      string `json:"job_desc"`
	TargetBranch string `json:"target_branch"`
}

func ResumeWorkflow(ctx workflow.Context, req ResumeWorkflowRequest) error {
	// Create new branch for us to work with
	var branchName string
	err := workflow.ExecuteChildWorkflow(
		ctx,
		agents.BranchNameAgent,
		agents.BranchNameAgentRequest{
			ClientOptions:  req.ClientOptions,
			JobDescription: req.JobDesc,
			Purpose:        agents.BranchNameAgentPurposeResume,
		},
	).Get(ctx, &branchName)
	if err != nil {
		return err
	}

	var pr int
	err = workflow.ExecuteChildWorkflow(
		ctx,
		agents.BuilderWorkflow,
		agents.BuilderAgentRequest{
			ClientOptions: req.ClientOptions,
			BuildTarget:   agents.BuildTargetResume,
			Builder:       agents.BuilderTypst,
			BranchName:    branchName,
			TargetBranch:  req.TargetBranch,
			Job:           req.JobDesc,
		},
	).Get(ctx, &pr)
	if err != nil {
		return err
	}

	err = workflow.ExecuteChildWorkflow(
		ctx,
		agents.ReviewAgent,
		agents.ReviewAgentArgs{
			Repo:       req.ClientOptions,
			Pr:         pr,
			BranchName: branchName,
		},
	).Get(ctx, &pr)
	if err != nil {
		return err
	}

	return nil
}
