package agents

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"go.temporal.io/sdk/workflow"

	"github.com/ansg191/job-temporal/internal/activities"
	"github.com/ansg191/job-temporal/internal/github"
	"github.com/ansg191/job-temporal/internal/llm"
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

	messages := []llm.Message{
		systemMessage(agentCfg.Instructions),
		userMessage(wrapLLMXML("repository", req.Owner+"/"+req.Repo)),
		userMessage(wrapLLMXML("branch", req.Branch)),
		userMessage(wrapLLMXML("pdf_url", pdfURL)),
		userMessage(wrapLLMXML("job_description", req.Job)),
	}

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: time.Second * 30,
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var aiTools []llm.ToolDefinition
	err = workflow.ExecuteActivity(ctx, activities.ListGithubTools).Get(ctx, &aiTools)
	if err != nil {
		return 0, err
	}
	conversation, err := createConversation(ctx, agentCfg.Model, nil)
	if err != nil {
		return 0, err
	}
	callAICtx := withCallAIActivityOptions(ctx)

	dispatcher := &githubDispatcher{aiTools: aiTools}

	for {
		var result activities.AIResponse
		err = workflow.ExecuteActivity(
			callAICtx,
			activities.CallAI,
			activities.AIRequest{
				Model:        agentCfg.Model,
				Input:        messages,
				Tools:        aiTools,
				Text:         prOutputFormat,
				Temperature:  temperatureOpt(agentCfg.Temperature),
				Conversation: conversation,
			},
		).Get(ctx, &result)
		if err != nil {
			return 0, err
		}
		conversation = result.Conversation

		if hasFunctionCalls(result.ToolCalls) {
			messages = tools.ProcessToolCalls(ctx, result.ToolCalls, dispatcher)
			continue
		}

		var pr prOutput
		if err = json.Unmarshal([]byte(result.OutputText), &pr); err != nil {
			messages = []llm.Message{
				userMessage("Invalid output format: " + err.Error()),
			}
			continue
		}
		if err = validatePRArtifactURL(pr.Body, pdfURL); err != nil {
			messages = []llm.Message{
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
	aiTools []llm.ToolDefinition
}

func (d *githubDispatcher) Dispatch(ctx workflow.Context, call llm.ToolCall) (workflow.Future, error) {
	if slices.ContainsFunc(d.aiTools, func(param llm.ToolDefinition) bool {
		return param.Name == call.Name
	}) {
		return workflow.ExecuteActivity(ctx, activities.CallGithubTool, call), nil
	}
	return nil, fmt.Errorf("unsupported tool: %s", call.Name)
}
