package agents

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/ansg191/job-temporal/internal/activities"
	"github.com/ansg191/job-temporal/internal/github"
	"github.com/ansg191/job-temporal/internal/llm"
	"github.com/ansg191/job-temporal/internal/tools"
)

type BuildTarget int

const (
	BuildTargetResume BuildTarget = iota
	BuildTargetCoverLetter
)

type BuilderAgentRequest struct {
	github.ClientOptions
	BuildTarget  BuildTarget `json:"build_target"`
	Builder      string      `json:"builder"`
	BranchName   string      `json:"branch_name"`
	TargetBranch string      `json:"target_branch"`
	Job          string      `json:"job"`
}

func BuilderAgent(ctx workflow.Context, req BuilderAgentRequest) (int, error) {
	agentName, ok := buildTargetAgentName[req.BuildTarget]
	if !ok {
		return 0, fmt.Errorf("invalid build target: %d", req.BuildTarget)
	}

	agentCfg, err := loadAgentConfig(ctx, agentName)
	if err != nil {
		return 0, err
	}

	messages := []llm.Message{
		systemMessage(agentCfg.Instructions),
		userMessage(wrapLLMXML("job_description", req.Job)),
		userMessage(wrapLLMXML("repository", req.Owner+"/"+req.Repo)),
		userMessage(wrapLLMXML("branch", req.BranchName)),
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
	layoutReviewRun := 0
	enableLayoutReview := req.BuildTarget == BuildTargetResume

	dispatcher := &builderDispatcher{
		aiTools:     aiTools,
		ghOpts:      req.ClientOptions,
		branchName:  req.BranchName,
		builder:     req.Builder,
		buildTarget: req.BuildTarget,
	}

	for {
		var result activities.AIResponse
		err = workflow.ExecuteActivity(
			callAICtx,
			activities.CallAI,
			activities.AIRequest{
				Model:        agentCfg.Model,
				Input:        messages,
				Tools:        availableBuilderTools(aiTools),
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

		if enableLayoutReview {
			// Layout review gate (resume only)
			file, err := resolveBuildTargetFile(req.BuildTarget)
			if err != nil {
				return 0, err
			}
			layoutReviewRun++
			layoutReviewReq := activities.ReviewPDFLayoutRequest{
				ClientOptions: req.ClientOptions,
				Branch:        req.BranchName,
				Builder:       req.Builder,
				File:          file,
				Notes:         result.OutputText,
			}
			layoutReviewResult, layoutReviewJSON, err := runLayoutReviewGate(
				ctx,
				MakeChildWorkflowID(ctx, "layout-review-gate", req.BranchName, strconv.Itoa(layoutReviewRun)),
				layoutReviewReq,
			)
			if err != nil {
				var appErr *temporal.ApplicationError
				if errors.As(err, &appErr) && appErr.Type() == activities.ErrTypeBuildFailed {
					var details []string
					_ = appErr.Details(&details)
					messages = []llm.Message{userMessage(fmt.Sprintf(
						"Build failed, fix and try again:\n%s",
						strings.Join(details, "\n"),
					))}
					continue
				}
				return 0, err
			}
			if block, reason := shouldBlockLayoutIssues(layoutReviewResult, layoutReviewRun); block {
				messages = []llm.Message{userMessage(
					"Layout review gate blocked completion (" + reason + "). Keep editing and rebuilding.\nCurrent findings JSON:\n" + layoutReviewJSON,
				)}
				continue
			}
		}

		// Activate PR Builder workflow
		var prNum int
		err = workflow.ExecuteChildWorkflow(
			workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
				WorkflowID: MakeChildWorkflowID(ctx, "pull-request-agent", req.BranchName, req.TargetBranch),
			}),
			PullRequestAgent,
			PullRequestAgentRequest{
				ClientOptions: req.ClientOptions,
				Branch:        req.BranchName,
				Target:        req.TargetBranch,
				Job:           req.Job,
				Builder:       req.Builder,
				BuildTarget:   req.BuildTarget,
			},
		).Get(ctx, &prNum)
		if err != nil {
			return 0, err
		}

		return prNum, nil
	}
}

type builderDispatcher struct {
	aiTools     []llm.ToolDefinition
	ghOpts      github.ClientOptions
	branchName  string
	builder     string
	buildTarget BuildTarget
}

func (d *builderDispatcher) Dispatch(ctx workflow.Context, call llm.ToolCall) (workflow.Future, error) {
	if slices.ContainsFunc(d.aiTools, func(param llm.ToolDefinition) bool {
		return param.Name == call.Name
	}) {
		return workflow.ExecuteActivity(ctx, activities.CallGithubTool, call), nil
	}

	switch call.Name {
	case tools.BuildToolDesc.Name:
		file, err := resolveBuildTargetFile(d.buildTarget)
		if err != nil {
			return nil, err
		}

		req := activities.BuildRequest{
			ClientOptions: d.ghOpts,
			Branch:        d.branchName,
			Builder:       d.builder,
			File:          file,
		}
		return workflow.ExecuteActivity(ctx, activities.Build, req), nil
	default:
		return nil, fmt.Errorf("unsupported tool: %s", call.Name)
	}
}

func availableBuilderTools(aiTools []llm.ToolDefinition) []llm.ToolDefinition {
	ret := append([]llm.ToolDefinition{}, aiTools...)
	ret = append(ret, tools.BuildToolDesc)
	return ret
}

// buildTargetAgentName maps BuildTarget to the YAML config agent name.
var buildTargetAgentName = map[BuildTarget]string{
	BuildTargetResume:      "builder_resume",
	BuildTargetCoverLetter: "builder_cover_letter",
}
