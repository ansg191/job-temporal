package workflows

import (
	"time"

	"go.temporal.io/sdk/workflow"

	"github.com/ansg191/job-temporal/internal/activities"
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
		workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			WorkflowID: agents.MakeChildWorkflowID(ctx, "branch-name-agent", "final"),
		}),
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
		workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			WorkflowID: agents.MakeChildWorkflowID(ctx, "builder-workflow", "resume", branchName),
		}),
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
		workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			WorkflowID: agents.MakeChildWorkflowID(ctx, "builder-workflow", "cover-letter", branchName),
		}),
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

	activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
	})
	err = workflow.ExecuteActivity(activityCtx, activities.ProtectBranch, activities.ProtectBranchRequest{
		ClientOptions: req.ClientOptions,
		Branch:        branchName,
	}).Get(activityCtx, nil)
	if err != nil {
		return "", err
	}

	return branchName, nil
}
