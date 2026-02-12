package workflows

import (
	"fmt"

	"go.temporal.io/sdk/workflow"

	"github.com/ansg191/job-temporal/internal/github"
	"github.com/ansg191/job-temporal/internal/workflows/agents"
)

type BuilderWorkflowRequest struct {
	github.ClientOptions
	JobDesc      string `json:"job_desc"`
	TargetBranch string `json:"target_branch"`
	Purpose      string `json:"purpose"`
	Builder      string `json:"builder"`
}

func BuilderWorkflow(ctx workflow.Context, req BuilderWorkflowRequest) error {
	purpose, buildTarget, err := resolvePurpose(req.Purpose)
	if err != nil {
		return err
	}

	// Create new branch for us to work with
	var branchName string
	err = workflow.ExecuteChildWorkflow(
		workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			WorkflowID: agents.MakeChildWorkflowID(ctx, "branch-name-agent", req.Purpose),
		}),
		agents.BranchNameAgent,
		agents.BranchNameAgentRequest{
			ClientOptions:  req.ClientOptions,
			JobDescription: req.JobDesc,
			Purpose:        purpose,
		},
	).Get(ctx, &branchName)
	if err != nil {
		return err
	}

	var pr int
	err = workflow.ExecuteChildWorkflow(
		workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			WorkflowID: agents.MakeChildWorkflowID(ctx, "builder-agent", branchName, req.Purpose),
		}),
		agents.BuilderAgent,
		agents.BuilderAgentRequest{
			ClientOptions: req.ClientOptions,
			BuildTarget:   buildTarget,
			Builder:       req.Builder,
			BranchName:    branchName,
			TargetBranch:  req.TargetBranch,
			Job:           req.JobDesc,
		},
	).Get(ctx, &pr)
	if err != nil {
		return err
	}

	err = workflow.ExecuteChildWorkflow(
		workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			WorkflowID: agents.MakeChildWorkflowID(ctx, "review-agent", branchName, req.Purpose),
		}),
		agents.ReviewAgent,
		agents.ReviewAgentArgs{
			Repo:        req.ClientOptions,
			Pr:          pr,
			BranchName:  branchName,
			BuildTarget: buildTarget,
		},
	).Get(ctx, &pr)
	if err != nil {
		return err
	}

	return nil
}

func resolvePurpose(purpose string) (agents.BranchNameAgentPurpose, agents.BuildTarget, error) {
	switch purpose {
	case "resume":
		return agents.BranchNameAgentPurposeResume, agents.BuildTargetResume, nil
	case "cover_letter":
		return agents.BranchNameAgentPurposeCoverLetter, agents.BuildTargetCoverLetter, nil
	default:
		return "", 0, fmt.Errorf("invalid purpose: %s", purpose)
	}
}
