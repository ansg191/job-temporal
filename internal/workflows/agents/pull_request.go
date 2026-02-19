package agents

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

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

const (
	purposeLabelResume      = "resume"
	purposeLabelCoverLetter = "cover letter"
)

type PullRequestAgentRequest struct {
	github.ClientOptions
	Branch      string      `json:"branch"`
	Target      string      `json:"target"`
	Job         string      `json:"job"`
	Builder     string      `json:"builder"`
	BuildTarget BuildTarget `json:"build_target"`
}

func PullRequestAgent(ctx workflow.Context, req PullRequestAgentRequest) (int, error) {
	agentCfg, err := loadAgentConfig(ctx, "pull_request")
	if err != nil {
		return 0, err
	}

	builderType := req.Builder
	if builderType == "" {
		builderType = "typst"
	}

	var pdfURL string
	err = workflow.ExecuteChildWorkflow(
		workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			WorkflowID: MakeChildWorkflowID(ctx, "build-upload-pdf", req.Branch, builderType),
		}),
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
		systemMessage(agentCfg.Instructions),
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
				Model:          agentCfg.Model,
				Input:          messages,
				Tools:          aiTools,
				Text:           prOutputFormat,
				Temperature:    temperatureOpt(agentCfg.Temperature),
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
		purposeLabel, err := purposeLabelForBuildTarget(req.BuildTarget)
		if err != nil {
			return 0, err
		}
		err = workflow.ExecuteActivity(ctx, activities.CreatePullRequest, activities.CreatePullRequestRequest{
			ClientOptions: req.ClientOptions,
			Title:         pr.Title,
			Description:   pr.Body,
			Head:          req.Branch,
			Base:          req.Target,
			PurposeLabel:  purposeLabel,
		}).Get(ctx, &prNum)
		if err != nil {
			return 0, err
		}
		return prNum, nil
	}
}

func purposeLabelForBuildTarget(target BuildTarget) (string, error) {
	switch target {
	case BuildTargetResume:
		return purposeLabelResume, nil
	case BuildTargetCoverLetter:
		return purposeLabelCoverLetter, nil
	default:
		return "", fmt.Errorf("invalid build target for pull request label: %d", target)
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
