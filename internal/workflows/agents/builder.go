package agents

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/openai/openai-go/v3/responses"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/ansg191/job-temporal/internal/activities"
	"github.com/ansg191/job-temporal/internal/github"
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

	messages := responses.ResponseInputParam{
		systemMessage(agentCfg.Instructions),
		userMessage("Remote: " + req.Owner + "/" + req.Repo),
		userMessage("Branch Name: " + req.BranchName),
		userMessage("Job Application:\n" + req.Job),
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
		var result *responses.Response
		err = workflow.ExecuteActivity(
			callAICtx,
			activities.CallAI,
			activities.OpenAIResponsesRequest{
				Model:          agentCfg.Model,
				Input:          messages,
				Tools:          availableBuilderTools(aiTools),
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
				Notes:         result.OutputText(),
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
					messages = responses.ResponseInputParam{userMessage(fmt.Sprintf(
						"Build failed, fix and try again:\n%s",
						strings.Join(details, "\n"),
					))}
					continue
				}
				return 0, err
			}
			if block, reason := shouldBlockLayoutIssues(layoutReviewResult, layoutReviewRun); block {
				messages = responses.ResponseInputParam{userMessage(
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
	aiTools     []responses.ToolUnionParam
	ghOpts      github.ClientOptions
	branchName  string
	builder     string
	buildTarget BuildTarget
}

func (d *builderDispatcher) Dispatch(ctx workflow.Context, call responses.ResponseOutputItemUnion) (workflow.Future, error) {
	if slices.ContainsFunc(d.aiTools, func(param responses.ToolUnionParam) bool {
		return param.OfFunction != nil && param.OfFunction.Name == call.Name
	}) {
		return workflow.ExecuteActivity(ctx, activities.CallGithubTool, call), nil
	}

	switch call.Name {
	case tools.BuildToolDesc.OfFunction.Name:
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

func availableBuilderTools(aiTools []responses.ToolUnionParam) []responses.ToolUnionParam {
	ret := append([]responses.ToolUnionParam{}, aiTools...)
	ret = append(ret, tools.BuildToolDesc)
	return ret
}

// buildTargetAgentName maps BuildTarget to the YAML config agent name.
var buildTargetAgentName = map[BuildTarget]string{
	BuildTargetResume:      "builder_resume",
	BuildTargetCoverLetter: "builder_cover_letter",
}
