package agents

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/ansg191/job-temporal/internal/activities"
	"github.com/ansg191/job-temporal/internal/config"
	"github.com/ansg191/job-temporal/internal/github"
	"github.com/ansg191/job-temporal/internal/llm"
	"github.com/ansg191/job-temporal/internal/tools"
	"github.com/ansg191/job-temporal/internal/webhook"
)

const reviewBotLogin = "job-temporal[bot]"

type ReviewAgentArgs struct {
	Repo        github.ClientOptions `json:"repo"`
	Pr          int                  `json:"pr"`
	BranchName  string               `json:"branch_name"`
	BuildTarget BuildTarget          `json:"build_target"`
}

func ReviewAgent(ctx workflow.Context, args ReviewAgentArgs) error {
	agentCfg, err := loadAgentConfig(ctx, "review_agent")
	if err != nil {
		return err
	}

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: time.Second * 30,
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	// Register ourselves as the review agent
	workflowID := workflow.GetInfo(ctx).WorkflowExecution.ID
	err = workflow.ExecuteActivity(
		ctx,
		activities.RegisterReviewReadyPR,
		workflowID,
		args.Repo.Owner,
		args.Repo.Repo,
		args.BranchName,
		args.Pr,
	).Get(ctx, nil)
	if err != nil {
		return err
	}

	reviewCh := workflow.GetSignalChannel(ctx, webhook.ReviewAgentSignal)
	rebuildCh := workflow.GetSignalChannel(ctx, webhook.RebuildSignal)

	var aiTools []llm.ToolDefinition
	err = workflow.ExecuteActivity(ctx, activities.ListGithubTools).Get(ctx, &aiTools)
	if err != nil {
		return err
	}

	conversation, err := createConversation(ctx, agentCfg.Model, []llm.Message{
		systemMessage(agentCfg.Instructions),
		userMessage(wrapLLMXML("repository", args.Repo.Owner+"/"+args.Repo.Repo)),
		userMessage(wrapLLMXML("branch", args.BranchName)),
		userMessage(wrapLLMXML("pr", strconv.Itoa(args.Pr))),
	})
	if err != nil {
		return err
	}
	buildRun := 0
	enableLayoutReview := args.BuildTarget == BuildTargetResume
	reviewProcessor := reviewSignalProcessor{
		args:               &args,
		agentCfg:           agentCfg,
		aiTools:            aiTools,
		conversation:       conversation,
		enableLayoutReview: enableLayoutReview,
		buildRun:           &buildRun,
	}

	for {
		var (
			reviewSignal   *webhook.WebhookSignal
			rebuildSignal  *webhook.WebhookSignal
			receivedReview bool
		)
		selector := workflow.NewSelector(ctx)
		selector.AddReceive(reviewCh, func(c workflow.ReceiveChannel, _ bool) {
			c.Receive(ctx, &reviewSignal)
			receivedReview = true
		})
		selector.AddReceive(rebuildCh, func(c workflow.ReceiveChannel, _ bool) {
			c.Receive(ctx, &rebuildSignal)
		})
		selector.Select(ctx)

		if receivedReview {
			shouldFinish, err := reviewProcessor.process(ctx, reviewSignal)
			if err != nil {
				return err
			}
			if shouldFinish {
				break
			}
			continue
		}

		if rebuildSignal == nil {
			continue
		}
		// Ignore pushes from ourselves.
		if rebuildSignal.AuthorLogin == reviewBotLogin {
			continue
		}

		buildRun++
		pdfURL, err := runBuildAndUploadForReview(ctx, args, buildRun)
		if err != nil {
			reason := buildFailureReason(err)
			workflow.GetLogger(ctx).Warn("Push rebuild failed", "reason", reason)
			if err = updatePRDescriptionFailure(ctx, args.Repo, args.Pr, reason); err != nil {
				return err
			}
			continue
		}
		if err = updatePRDescriptionSuccess(ctx, args.Repo, args.Pr, pdfURL); err != nil {
			return err
		}
	}

	// Mark PR as finished
	return workflow.ExecuteActivity(ctx, activities.FinishReview, workflowID).Get(ctx, nil)
}

type reviewSignalProcessor struct {
	args               *ReviewAgentArgs
	agentCfg           *config.AgentConfig
	aiTools            []llm.ToolDefinition
	conversation       *llm.ConversationState
	enableLayoutReview bool
	buildRun           *int
}

func (p *reviewSignalProcessor) process(ctx workflow.Context, reviewSignal *webhook.WebhookSignal) (bool, error) {
	if reviewSignal == nil {
		return false, nil
	}
	// If signal is PR closed, then we are done.
	if reviewSignal.Type == "pull_request" && reviewSignal.Action == "closed" {
		return true, nil
	}
	// Ignore signals from ourselves.
	if reviewSignal.AuthorLogin == reviewBotLogin {
		return false, nil
	}
	// Ignore signals with empty bodies.
	if reviewSignal.Body == "" {
		return false, nil
	}

	signalBytes, err := json.Marshal(reviewSignal)
	if err != nil {
		return false, err
	}
	workflow.GetLogger(ctx).Info("Received signal: " + string(signalBytes))
	pendingInput := []llm.Message{
		userMessage(wrapLLMXML("reviewer_comment", string(signalBytes))),
	}
	callAICtx := withCallAIActivityOptions(ctx)
	dispatcher := &reviewAgentDispatcher{
		aiTools:     p.aiTools,
		ghOpts:      p.args.Repo,
		branchName:  p.args.BranchName,
		buildTarget: p.args.BuildTarget,
	}

	var pdfURL string
	for {
		var result activities.AIResponse
		err = workflow.ExecuteActivity(
			callAICtx,
			activities.CallAI,
			activities.AIRequest{
				Model:        p.agentCfg.Model,
				Input:        pendingInput,
				Tools:        availableReviewTools(p.aiTools, p.enableLayoutReview),
				Temperature:  temperatureOpt(p.agentCfg.Temperature),
				Conversation: p.conversation,
			},
		).Get(ctx, &result)
		if err != nil {
			return false, err
		}
		p.conversation = result.Conversation

		if !hasFunctionCalls(result.ToolCalls) {
			// Finished with agent loop, rebuild the PDF.
			*p.buildRun++
			pdfURL, err = runBuildAndUploadForReview(ctx, *p.args, *p.buildRun)
			if err != nil {
				var appErr *temporal.ApplicationError
				if errors.As(err, &appErr) && appErr.Type() == activities.ErrTypeBuildFailed {
					// Build failed, so kick back to AI to fix.
					var details []string
					_ = appErr.Details(&details)
					pendingInput = []llm.Message{userMessage(fmt.Sprintf(
						"Build failed, fix and try again: \n%s",
						strings.Join(details, "\n"),
					))}
					continue
				}
				return false, err
			}
			break
		}

		pendingInput = tools.ProcessToolCalls(ctx, result.ToolCalls, dispatcher)
	}

	// Update URL in PR description.
	if err = updatePRDescriptionSuccess(ctx, p.args.Repo, p.args.Pr, pdfURL); err != nil {
		return false, err
	}

	return false, nil
}

type reviewAgentDispatcher struct {
	aiTools     []llm.ToolDefinition
	ghOpts      github.ClientOptions
	branchName  string
	buildTarget BuildTarget
}

func (d *reviewAgentDispatcher) Dispatch(ctx workflow.Context, call llm.ToolCall) (workflow.Future, error) {
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
			Builder:       "typst", // TODO: remove this hardcoded builder
			File:          file,
		}
		return workflow.ExecuteActivity(ctx, activities.Build, req), nil
	case tools.ReviewPDFLayoutToolDesc.Name:
		if d.buildTarget != BuildTargetResume {
			return nil, fmt.Errorf("review_pdf_layout is only available for resume builds")
		}
		args := tools.ReviewPDFLayoutArgs{}
		if err := tools.ReviewPDFLayoutToolParseArgs(call.Arguments, &args); err != nil {
			return nil, err
		}

		file, err := resolveBuildTargetFile(d.buildTarget)
		if err != nil {
			return nil, err
		}

		req := activities.ReviewPDFLayoutRequest{
			ClientOptions: d.ghOpts,
			Branch:        d.branchName,
			Builder:       "typst", // TODO: remove this hardcoded builder
			File:          file,
			PageStart:     args.PageStart,
			PageEnd:       args.PageEnd,
			Notes:         args.Notes,
		}
		return workflow.ExecuteChildWorkflow(
			workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
				WorkflowID: MakeChildWorkflowID(ctx, "review-pdf-layout", d.branchName, call.CallID),
			}),
			ReviewPDFLayoutWorkflow,
			req,
		), nil
	default:
		return nil, fmt.Errorf("unsupported tool: %s", call.Name)
	}
}

func availableReviewTools(aiTools []llm.ToolDefinition, enableLayoutReview bool) []llm.ToolDefinition {
	ret := append([]llm.ToolDefinition{}, aiTools...)
	ret = append(ret, tools.BuildToolDesc)
	if enableLayoutReview {
		ret = append(ret, tools.ReviewPDFLayoutToolDesc)
	}
	return ret
}

const prArtifactErrorLinePrefix = "PDF Artifact Error:"

func runBuildAndUploadForReview(ctx workflow.Context, args ReviewAgentArgs, buildRun int) (string, error) {
	var pdfURL string
	err := workflow.ExecuteChildWorkflow(
		workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			WorkflowID: MakeChildWorkflowID(
				ctx,
				"build-upload-pdf",
				args.BranchName,
				strconv.Itoa(args.Pr),
				strconv.Itoa(buildRun),
			),
		}),
		BuildAndUploadPDFWorkflow,
		BuildAndUploadPDFWorkflowRequest{
			ClientOptions: args.Repo,
			Branch:        args.BranchName,
			Builder:       "typst", // TODO: remove this hardcoded builder
			BuildTarget:   args.BuildTarget,
		},
	).Get(ctx, &pdfURL)
	if err != nil {
		return "", err
	}
	return pdfURL, nil
}

func updatePRDescriptionSuccess(ctx workflow.Context, repo github.ClientOptions, pr int, url string) error {
	// Get existing PR description
	var body string
	err := workflow.ExecuteActivity(
		ctx,
		activities.GetPullRequestBody,
		activities.GetPullRequestBodyRequest{
			ClientOptions: repo,
			PRNumber:      pr,
		},
	).Get(ctx, &body)
	if err != nil {
		return err
	}

	body, oldURL := rewriteArtifactLinesForSuccess(body, url)
	if oldURL != "" {
		err = workflow.ExecuteActivity(
			ctx,
			activities.DeletePDFByURL,
			activities.DeletePDFByURLRequest{URL: oldURL},
		).Get(ctx, nil)
		if err != nil {
			return err
		}
	}

	// Update PR description
	return workflow.ExecuteActivity(
		ctx,
		activities.UpdatePullRequestBody,
		activities.UpdatePullRequestBodyRequest{
			ClientOptions: repo,
			PRNumber:      pr,
			Body:          body,
		},
	).Get(ctx, nil)
}

func updatePRDescriptionFailure(ctx workflow.Context, repo github.ClientOptions, pr int, reason string) error {
	var body string
	err := workflow.ExecuteActivity(
		ctx,
		activities.GetPullRequestBody,
		activities.GetPullRequestBodyRequest{
			ClientOptions: repo,
			PRNumber:      pr,
		},
	).Get(ctx, &body)
	if err != nil {
		return err
	}

	body = rewriteArtifactLinesForFailure(body, reason)
	return workflow.ExecuteActivity(
		ctx,
		activities.UpdatePullRequestBody,
		activities.UpdatePullRequestBodyRequest{
			ClientOptions: repo,
			PRNumber:      pr,
			Body:          body,
		},
	).Get(ctx, nil)
}

func rewriteArtifactLinesForSuccess(body, url string) (string, string) {
	lines := strings.Split(body, "\n")
	next := make([]string, 0, len(lines)+1)
	artifactFound := false
	oldURL := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, prArtifactErrorLinePrefix):
			// Clear stale error lines after a successful rebuild.
			continue
		case strings.HasPrefix(trimmed, prArtifactLinePrefix):
			if !artifactFound {
				oldURL = strings.TrimSpace(trimmed[len(prArtifactLinePrefix):])
				next = append(next, prArtifactLinePrefix+" "+url)
				artifactFound = true
			}
			continue
		default:
			next = append(next, line)
		}
	}
	if !artifactFound {
		next = append(next, prArtifactLinePrefix+" "+url)
	}
	return strings.Join(next, "\n"), oldURL
}

func rewriteArtifactLinesForFailure(body, reason string) string {
	reason = sanitizeArtifactErrorReason(reason)
	lines := strings.Split(body, "\n")
	next := make([]string, 0, len(lines)+2)
	artifactFound := false
	errorFound := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, prArtifactLinePrefix):
			if !artifactFound {
				next = append(next, prArtifactLinePrefix)
				artifactFound = true
			}
			continue
		case strings.HasPrefix(trimmed, prArtifactErrorLinePrefix):
			if !errorFound {
				next = append(next, prArtifactErrorLinePrefix+" "+reason)
				errorFound = true
			}
			continue
		default:
			next = append(next, line)
		}
	}
	if !artifactFound {
		next = append(next, prArtifactLinePrefix)
	}
	if !errorFound {
		next = append(next, prArtifactErrorLinePrefix+" "+reason)
	}
	return strings.Join(next, "\n")
}

func sanitizeArtifactErrorReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "build failed"
	}
	reason = strings.ReplaceAll(reason, "\n", " ")
	reason = strings.ReplaceAll(reason, "\r", " ")
	reason = strings.Join(strings.Fields(reason), " ")
	const maxLen = 200
	if len(reason) > maxLen {
		return reason[:maxLen]
	}
	return reason
}

func buildFailureReason(err error) string {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) && appErr.Type() == activities.ErrTypeBuildFailed {
		var details []string
		if detailsErr := appErr.Details(&details); detailsErr == nil && len(details) > 0 {
			return sanitizeArtifactErrorReason(details[0])
		}
	}
	return sanitizeArtifactErrorReason(err.Error())
}
