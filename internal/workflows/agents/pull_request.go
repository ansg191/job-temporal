package agents

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/openai/openai-go/v3"
	"go.temporal.io/sdk/workflow"

	"github.com/ansg191/job-temporal/internal/activities"
	"github.com/ansg191/job-temporal/internal/github"
	"github.com/ansg191/job-temporal/internal/tools"
)

type prOutput struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

var prOutputFormat = activities.GenerateResponseFormat[prOutput]("pr_output")

const PullRequestInstructions = `
You are a pull request agent who creates pull requests for the repository.
These pull requests are created from branches that were created by previous agents.
These agent's took a job application, and tailored a resume for that specific job application.

CORE RESPONSIBILITIES:
1. Given a branch name, create a pull request description and title for the repository.
2. Give the pull request a descriptive title and description based on the changes made in the branch.
3. Be very descriptive in the pull request description. Try to explain every change and why it was done using the 
changes, commit history, & job description.
4. Return the description and title in the provided output format.

IMPORTANT NOTES:
- Only work in the repository provided
- Only work in the branch provided
- DO NOT under ANY circumstance create the pull request yourself using the Github tools.
The pull request will be made using the title and description returned by you.

AVAILABLE TOOLS:
- Github MCP tools to help you understand the changes made in the branch.
Use these tools to get information about the changes made in the branch.

OUTPUT FORMAT:
When you are ready to create the pull request, respond with a JSON object with the following format:
{
	"title": "Pull request title",
	"body": "Pull request description. Make sure to use escape characters"
}
This will be used to create the pull request.
`

type PullRequestAgentRequest struct {
	github.ClientOptions
	Branch string `json:"branch"`
	Target string `json:"target"`
	Job    string `json:"job"`
}

func PullRequestAgent(ctx workflow.Context, req PullRequestAgentRequest) (int, error) {
	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(PullRequestInstructions),
		openai.UserMessage("Remote: " + req.Owner + "/" + req.Repo),
		openai.UserMessage("Branch Name: " + req.Branch),
		openai.UserMessage("Job description: " + req.Job),
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

	dispatcher := &githubDispatcher{aiTools: aiTools}

	for {
		var result *openai.ChatCompletion
		err = workflow.ExecuteActivity(
			ctx,
			activities.CallAI,
			activities.OpenAIResponsesRequest{
				Model:          openai.ChatModelGPT5_2,
				Messages:       messages,
				Tools:          aiTools,
				ResponseFormat: prOutputFormat,
			},
		).Get(ctx, &result)
		if err != nil {
			return 0, err
		}

		messages = append(messages, result.Choices[0].Message.ToParam())

		if result.Choices[0].FinishReason == "tool_calls" {
			toolMsgs := tools.ProcessToolCalls(ctx, result.Choices[0].Message.ToolCalls, dispatcher)
			messages = append(messages, toolMsgs...)
			continue
		}

		var pr prOutput
		if err = json.Unmarshal([]byte(result.Choices[0].Message.Content), &pr); err != nil {
			messages = append(messages, openai.UserMessage("Invalid output format: "+err.Error()))
			continue
		}

		var prNum int
		err = workflow.ExecuteActivity(ctx, activities.CreatePullRequest, activities.CreatePullRequestRequest{
			ClientOptions: req.ClientOptions,
			Title:         pr.Title,
			Description:   pr.Body,
			Head:          req.Branch,
			Base:          req.Target,
		}).Get(ctx, &prNum)
		if err != nil {
			return 0, err
		}
		return prNum, nil
	}
}

type githubDispatcher struct {
	aiTools []openai.ChatCompletionToolUnionParam
}

func (d *githubDispatcher) Dispatch(ctx workflow.Context, call openai.ChatCompletionMessageToolCallUnion) (workflow.Future, error) {
	if slices.ContainsFunc(d.aiTools, func(param openai.ChatCompletionToolUnionParam) bool {
		return param.GetFunction().Name == call.Function.Name
	}) {
		return workflow.ExecuteActivity(ctx, activities.CallGithubTool, call), nil
	}
	return nil, fmt.Errorf("unsupported tool: %s", call.Function.Name)
}
