package agents

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"go.temporal.io/sdk/workflow"

	"github.com/ansg191/job-temporal/internal/activities"
	"github.com/ansg191/job-temporal/internal/tools"
)

const PullRequestInstructions = `
You are a pull request agent who creates pull requests for the repository.
These pull requests are created from branches that were created by previous agents.
These agent's took a job application, and tailored a resume for that specific job application.

CORE RESPONSIBILITIES:
1. Given a branch name, create a pull request for the repository.
2. Give the pull request a descriptive title and description based on the changes made in the branch.
3. Be very descriptive in the pull request description. Try to explain every change and why it was done using the 
changes, commit history, & job description.
4. Return the pull request number.

IMPORTANT NOTES:
- Only work in the repository provided
- Only work in the branch provided

AVAILABLE TOOLS:
- Github MCP tools to create pull requests in the repository.
`

func PullRequestAgent(ctx workflow.Context, owner, repo, branch, job string) (int, error) {
	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(PullRequestInstructions),
		openai.UserMessage("Remote: " + owner + "/" + repo),
		openai.UserMessage("Branch Name: " + branch),
		openai.UserMessage("Job description: " + job),
	}

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: time.Second * 30,
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var aiTools []openai.ChatCompletionToolUnionParam
	err := workflow.ExecuteActivity(ctx, activities.ListGithubTools).Get(ctx, &aiTools)
	if err != nil {
		return 0, err
	}

	for {
		var result *openai.ChatCompletion
		err := workflow.ExecuteActivity(
			ctx,
			activities.CallAI,
			activities.OpenAIResponsesRequest{
				Model:    openai.ChatModelGPT5_2,
				Messages: messages,
				Tools:    aiTools,
			},
		).Get(ctx, &result)
		if err != nil {
			return 0, err
		}

		messages = append(messages, result.Choices[0].Message.ToParam())

		if result.Choices[0].FinishReason == "tool_calls" {
			futs := make([]workflow.Future, len(result.Choices[0].Message.ToolCalls))
			for i, call := range result.Choices[0].Message.ToolCalls {
				name := call.Function.Name
				if slices.ContainsFunc(aiTools, func(param openai.ChatCompletionToolUnionParam) bool {
					tName := param.GetFunction().Name
					return tName == name
				}) {
					futs[i] = workflow.ExecuteActivity(ctx, activities.CallGithubTool, call)
				} else {
					messages = append(messages, openai.ToolMessage("Unsupported tool: "+name, call.ID))
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
			prString := strings.TrimPrefix(result.Choices[0].Message.Content, "#")
			prNum, err := strconv.ParseInt(prString, 10, 32)
			if err != nil {
				messages = append(messages, openai.UserMessage(
					fmt.Sprintf("Unable to parse pull request number %s", err.Error())))
				continue
			}
			return int(prNum), nil
		}
	}
}
