package agents

import (
	"fmt"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/ansg191/job-temporal/internal/activities"
	"github.com/ansg191/job-temporal/internal/github"
	"github.com/ansg191/job-temporal/internal/tools"
)

const branchNameAgentInstructions = `
You are an expert git branch namer. Your job is to create concise, descriptive,
and standardized branch names based on a job posting description.
This branch name will be used to modify the resume or cover letter of a job applicant to better fit.

CORE RESPONSIBILITIES:
1. Read the job posting description carefully.
2. Create a unique, concise, & human-readable branch name that reflects the job role.
3. Create separate branches for the specified purpose (resume, cover letter, final).
4. Use lowercase letters, numbers, and hyphens only.
5. Avoid special characters, spaces, or underscores.
6. Keep the branch name under 16 characters.

AVAILABLE TOOLS:
- list_branches(): List existing branch names. Avoid duplicates.

OUTPUT FORMAT:
Respond with only the branch name as a single string, without any additional text or formatting.
`

type BranchNameAgentPurpose string

const (
	BranchNameAgentPurposeResume      BranchNameAgentPurpose = "resume"
	BranchNameAgentPurposeCoverLetter BranchNameAgentPurpose = "cover_letter"
	BranchNameAgentPurposeFinal       BranchNameAgentPurpose = "final"
)

type BranchNameAgentRequest struct {
	github.ClientOptions
	JobDescription string                 `json:"job_description"`
	Purpose        BranchNameAgentPurpose `json:"purpose"`
}

func BranchNameAgent(ctx workflow.Context, req BranchNameAgentRequest) (string, error) {
	if req.Purpose != BranchNameAgentPurposeResume &&
		req.Purpose != BranchNameAgentPurposeCoverLetter &&
		req.Purpose != BranchNameAgentPurposeFinal {
		return "", temporal.NewNonRetryableApplicationError("invalid purpose", "InvalidPurpose", nil)
	}

	messages := responses.ResponseInputParam{
		systemMessage(branchNameAgentInstructions),
		userMessage("Purpose: " + string(req.Purpose)),
		userMessage("Job Description:\n" + req.JobDescription),
	}

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	dispatcher := &branchNameDispatcher{ghOpts: req.ClientOptions}

	for range 5 {
		var result *responses.Response
		err := workflow.ExecuteActivity(
			ctx,
			activities.CallAI,
			activities.OpenAIResponsesRequest{
				Model: openai.ChatModelGPT5_2,
				Input: messages,
				Tools: []responses.ToolUnionParam{
					tools.ListBranchesToolDesc,
				},
				Temperature: openai.Float(0),
			},
		).Get(ctx, &result)
		if err != nil {
			return "", err
		}

		messages = appendOutput(messages, result.Output)

		if hasFunctionCalls(result.Output) {
			toolMsgs := tools.ProcessToolCalls(ctx, filterFunctionCalls(result.Output), dispatcher)
			messages = append(messages, toolMsgs...)
			continue
		}

		branchName := result.OutputText()

		req := activities.CreateBranchRequest{
			ClientOptions: req.ClientOptions,
			Branch:        branchName,
		}
		err = workflow.ExecuteActivity(ctx, activities.CreateBranch, req).Get(ctx, nil)
		if err != nil {
			messages = append(messages, userMessage("Unable to create branch: "+err.Error()+"\n"))
			continue
		}
		return branchName, nil
	}

	return "", temporal.NewNonRetryableApplicationError("failed to generate branch name", "BranchNameError", nil)
}

type branchNameDispatcher struct {
	ghOpts github.ClientOptions
}

func (d *branchNameDispatcher) Dispatch(ctx workflow.Context, call responses.ResponseOutputItemUnion) (workflow.Future, error) {
	switch call.Name {
	case tools.ListBranchesToolDesc.OfFunction.Name:
		req := activities.ListBranchesRequest{ClientOptions: d.ghOpts}
		return workflow.ExecuteActivity(ctx, activities.ListBranches, req), nil
	default:
		return nil, fmt.Errorf("unsupported tool: %s", call.Name)
	}
}
