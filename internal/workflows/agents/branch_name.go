package agents

import (
	"fmt"
	"time"

	"github.com/openai/openai-go/v3"
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

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(branchNameAgentInstructions),
		openai.UserMessage("Purpose: " + string(req.Purpose)),
		openai.UserMessage("Job Description:\n" + req.JobDescription),
	}

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	dispatcher := &branchNameDispatcher{ghOpts: req.ClientOptions}

	for range 5 {
		var result *openai.ChatCompletion
		err := workflow.ExecuteActivity(
			ctx,
			activities.CallAI,
			activities.OpenAIResponsesRequest{
				Model:    openai.ChatModelGPT5_2,
				Messages: messages,
				Tools: []openai.ChatCompletionToolUnionParam{
					tools.ListBranchesToolDesc,
				},
				Temperature: openai.Float(0),
			},
		).Get(ctx, &result)
		if err != nil {
			return "", err
		}

		messages = append(messages, result.Choices[0].Message.ToParam())

		if result.Choices[0].FinishReason == "tool_calls" {
			toolMsgs := tools.ProcessToolCalls(ctx, result.Choices[0].Message.ToolCalls, dispatcher)
			messages = append(messages, toolMsgs...)
			continue
		}

		branchName := result.Choices[0].Message.Content

		req := activities.CreateBranchRequest{
			ClientOptions: req.ClientOptions,
			Branch:        branchName,
		}
		err = workflow.ExecuteActivity(ctx, activities.CreateBranch, req).Get(ctx, nil)
		if err != nil {
			messages = append(messages, openai.UserMessage("Unable to create branch: "+err.Error()+"\n"))
			continue
		}
		return branchName, nil
	}

	return "", temporal.NewNonRetryableApplicationError("failed to generate branch name", "BranchNameError", nil)
}

type branchNameDispatcher struct {
	ghOpts github.ClientOptions
}

func (d *branchNameDispatcher) Dispatch(ctx workflow.Context, call openai.ChatCompletionMessageToolCallUnion) (workflow.Future, error) {
	switch call.Function.Name {
	case tools.ListBranchesToolDesc.OfFunction.Function.Name:
		req := activities.ListBranchesRequest{ClientOptions: d.ghOpts}
		return workflow.ExecuteActivity(ctx, activities.ListBranches, req), nil
	default:
		return nil, fmt.Errorf("unsupported tool: %s", call.Function.Name)
	}
}
