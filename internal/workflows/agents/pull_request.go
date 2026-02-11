package agents

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"go.temporal.io/sdk/workflow"

	"github.com/ansg191/job-temporal/internal/activities"
	"github.com/ansg191/job-temporal/internal/github"
	"github.com/ansg191/job-temporal/internal/tools"
)

type prOutput struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

var prOutputFormat = activities.GenerateTextFormat[prOutput]("pr_output")

const prArtifactLinePrefix = "PDF Artifact:"

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
- Always include the provided public PDF URL in the pull request description.
- Include it on its own line in this exact format: "PDF Artifact: <url>".

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
	Branch      string      `json:"branch"`
	Target      string      `json:"target"`
	Job         string      `json:"job"`
	Builder     string      `json:"builder"`
	BuildTarget BuildTarget `json:"build_target"`
}

func PullRequestAgent(ctx workflow.Context, req PullRequestAgentRequest) (int, error) {
	builderType := req.Builder
	if builderType == "" {
		builderType = "typst"
	}

	var pdfURL string
	err := workflow.ExecuteChildWorkflow(
		ctx,
		BuildAndUploadPDFWorkflow,
		BuildAndUploadPDFWorkflowRequest{
			ClientOptions: req.ClientOptions,
			Branch:        req.Branch,
			Builder:       builderType,
			BuildTarget:   req.BuildTarget,
		},
	).Get(ctx, &pdfURL)
	if err != nil {
		return 0, err
	}

	messages := responses.ResponseInputParam{
		systemMessage(PullRequestInstructions),
		userMessage("Remote: " + req.Owner + "/" + req.Repo),
		userMessage("Branch Name: " + req.Branch),
		userMessage("Job description: " + req.Job),
		userMessage("Public PDF URL (must be included in PR description): " + pdfURL),
	}

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: time.Second * 30,
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var aiTools []responses.ToolUnionParam
	err = workflow.ExecuteActivity(ctx, activities.ListGithubTools).Get(ctx, &aiTools)
	if err != nil {
		return 0, err
	}
	conversationID, err := createConversation(ctx, nil)
	if err != nil {
		return 0, err
	}
	callAICtx := withCallAIActivityOptions(ctx)

	dispatcher := &githubDispatcher{aiTools: aiTools}

	for {
		var result *responses.Response
		err = workflow.ExecuteActivity(
			callAICtx,
			activities.CallAI,
			activities.OpenAIResponsesRequest{
				Model:          openai.ChatModelGPT5_2,
				Input:          messages,
				Tools:          aiTools,
				Text:           prOutputFormat,
				ConversationID: conversationID,
			},
		).Get(ctx, &result)
		if err != nil {
			return 0, err
		}

		if hasFunctionCalls(result.Output) {
			messages = tools.ProcessToolCalls(ctx, filterFunctionCalls(result.Output), dispatcher)
			continue
		}

		var pr prOutput
		if err = json.Unmarshal([]byte(result.OutputText()), &pr); err != nil {
			messages = responses.ResponseInputParam{
				userMessage("Invalid output format: " + err.Error()),
			}
			continue
		}
		if err = validatePRArtifactURL(pr.Body, pdfURL); err != nil {
			messages = responses.ResponseInputParam{
				userMessage("Invalid PR body: " + err.Error()),
			}
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

func validatePRArtifactURL(body string, url string) error {
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, prArtifactLinePrefix) {
			if trimmed != prArtifactLinePrefix+" "+url {
				return fmt.Errorf("artifact line must be exactly %q", prArtifactLinePrefix+" "+url)
			}
			return nil
		}
	}
	return fmt.Errorf("missing artifact line %q", prArtifactLinePrefix+" "+url)
}

type githubDispatcher struct {
	aiTools []responses.ToolUnionParam
}

func (d *githubDispatcher) Dispatch(ctx workflow.Context, call responses.ResponseOutputItemUnion) (workflow.Future, error) {
	if slices.ContainsFunc(d.aiTools, func(param responses.ToolUnionParam) bool {
		return param.OfFunction != nil && param.OfFunction.Name == call.Name
	}) {
		return workflow.ExecuteActivity(ctx, activities.CallGithubTool, call), nil
	}
	return nil, fmt.Errorf("unsupported tool: %s", call.Name)
}
