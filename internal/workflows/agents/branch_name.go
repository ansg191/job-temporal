package agents

import (
	"time"

	"github.com/openai/openai-go/v3"
	"go.temporal.io/sdk/workflow"

	"github.com/ansg191/job-temporal/internal/activities"
	"github.com/ansg191/job-temporal/internal/tools"
)

const branchNameAgentInstructions = `
You are an expert git branch namer. Your job is to create concise, descriptive,
and standardized branch names based on a job posting description.
This branch name will be used to modify the resume of a job applicant to better fit.

CORE RESPONSIBILITIES:
1. Read the job posting description carefully.
2. Create a unique, concise, & human-readable branch name that reflects the job role.
3. Use lowercase letters, numbers, and hyphens only.
4. Avoid special characters, spaces, or underscores.
5. Keep the branch name under 16 characters.

AVAILABLE TOOLS:
- list_branches(): List existing branch names. Avoid duplicates.

OUTPUT FORMAT:
Respond with only the branch name as a single string, without any additional text or formatting.
`

func BranchNameAgent(ctx workflow.Context, remote, jobDescription string) (string, error) {
	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(branchNameAgentInstructions),
		openai.UserMessage("Job Description:\n" + jobDescription),
	}

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	for {
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
			futs := make([]workflow.Future, len(result.Choices[0].Message.ToolCalls))
			for i, call := range result.Choices[0].Message.ToolCalls {
				name := call.Function.Name
				//args := call.Function.Arguments

				switch name {
				case tools.ListBranchesToolDesc.OfFunction.Function.Name:
					req := activities.ListBranchesRequest{RepoRemote: remote}
					futs[i] = workflow.ExecuteActivity(ctx, activities.ListBranches, req)
				}
			}

			for i, fut := range futs {
				if fut == nil {
					continue
				}

				res, err := tools.GetToolResult(ctx, fut, result.Choices[0].Message.ToolCalls[i].ID)
				if err != nil {
					messages = append(messages, openai.ToolMessage(err.Error(), result.Choices[0].Message.ToolCalls[i].ID))
					continue
				}
				messages = append(messages, res)
			}
		} else {
			branchName := result.Choices[0].Message.Content

			err = workflow.ExecuteActivity(ctx, activities.CreateBranch, remote, branchName).Get(ctx, nil)
			if err != nil {
				messages = append(messages, openai.UserMessage("Unable to create branch: "+err.Error()+"\n"))
				continue
			}
			return branchName, nil
		}
	}
}
