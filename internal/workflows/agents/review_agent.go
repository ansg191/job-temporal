package agents

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/ansg191/job-temporal/internal/activities"
	"github.com/ansg191/job-temporal/internal/github"
	"github.com/ansg191/job-temporal/internal/tools"
	"github.com/ansg191/job-temporal/internal/webhook"
)

const ReviewAgentInstructions = `
You are a review agent who reviews pull requests on GitHub.
A previous agent before you created this pull request to modify
a resume to better tailor towards a specific job application.
You will interact with a human reviewer who will leave feedback on the pull
request.
Your job is to use that feedback to make further improvements to the resume
until the human reviewer is satisfied and closes the pull request.

CORE RESPONSIBILITIES:
1. Read the pull request description, changes, and commits carefully
to understand how and why changes were made.
2. On human reviewer comment, analyze the reviewer's feedback.
3. Make changes to the resume based on the feedback, if necessary.
4. Respond to the reviewer's comments explaining the changes you made or
asking for clarification if needed.
5. Continue this process until the pull request is closed.

IMPORTANT NOTES:
- Only work in the pull request branch provided.
- Only make changes that are necessary based on the reviewer's feedback.
- Be polite and professional in your responses to the reviewer.
- Under no circumstance use the github MCP to read reviewer comments (get_review_comments method) 
  only use the user messages provided to you.
- Keep iterating on layout quality as long as practical.
- Always fix all high severity layout issues before stopping.
- Make sure to pass notes of your changes to issues in review_pdf_layout's notes
- If an issue CANNOT be solved without changing formatting (editing resume.typ), you MUST ignore it and output
a note explaining why for the review workflow.

AVAILABLE TOOLS:
- Github MCP tools to read and edit files in the applicant's resume repository.
- build(): Compile the resume and perform various checks
- review_pdf_layout(): Render built pages and return structured visual layout defects with fix hints (resume only).
`

type ReviewAgentArgs struct {
	Repo        github.ClientOptions `json:"repo"`
	Pr          int                  `json:"pr"`
	BranchName  string               `json:"branch_name"`
	BuildTarget BuildTarget          `json:"build_target"`
}

func ReviewAgent(ctx workflow.Context, args ReviewAgentArgs) error {
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: time.Second * 30,
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	// Register ourselves as the review agent
	workflowId := workflow.GetInfo(ctx).WorkflowExecution.ID
	err := workflow.ExecuteActivity(ctx, activities.RegisterReviewReadyPR, workflowId, args.Pr).Get(ctx, nil)
	if err != nil {
		return err
	}

	// Get signal channel
	ch := workflow.GetSignalChannel(ctx, webhook.ReviewAgentSignal)

	messages := responses.ResponseInputParam{
		systemMessage(ReviewAgentInstructions),
		userMessage("Remote: " + args.Repo.Owner + "/" + args.Repo.Repo),
		userMessage("Pull Request: " + strconv.Itoa(args.Pr)),
		userMessage("Branch Name: " + args.BranchName),
	}

	var aiTools []responses.ToolUnionParam
	err = workflow.ExecuteActivity(ctx, activities.ListGithubTools).Get(ctx, &aiTools)
	if err != nil {
		return err
	}
	conversationID, err := createConversation(ctx, nil)
	if err != nil {
		return err
	}
	callAICtx := withCallAIActivityOptions(ctx)
	initialized := false
	buildRun := 0
	enableLayoutReview := args.BuildTarget == BuildTargetResume

	for {
		// Wait for signal
		var signal *webhook.WebhookSignal
		ch.Receive(ctx, &signal)

		if signal == nil {
			continue
		}
		// If signal is PR closed, then we are done
		if signal.Type == "pull_request" && signal.Action == "closed" {
			break
		}
		// Ignore signals from ourselves
		if signal.AuthorLogin == "job-temporal[bot]" {
			continue
		}
		// Ignore signals with empty bodies
		if signal.Body == "" {
			continue
		}

		signalBytes, err := json.Marshal(signal)
		if err != nil {
			return err
		}
		workflow.GetLogger(ctx).Info("Received signal: " + string(signalBytes))
		pendingInput := responses.ResponseInputParam{
			userMessage("User Review: " + string(signalBytes)),
		}
		if !initialized {
			pendingInput = append(messages, pendingInput...)
			initialized = true
		}

		dispatcher := &reviewAgentDispatcher{
			aiTools:     aiTools,
			ghOpts:      args.Repo,
			branchName:  args.BranchName,
			buildTarget: args.BuildTarget,
		}

		var pdfUrl string
		for {
			var result *responses.Response
			err = workflow.ExecuteActivity(
				callAICtx,
				activities.CallAI,
				activities.OpenAIResponsesRequest{
					Model:          openai.ChatModelGPT5_2,
					Input:          pendingInput,
					Tools:          availableReviewTools(aiTools, enableLayoutReview),
					ConversationID: conversationID,
				},
			).Get(ctx, &result)
			if err != nil {
				return err
			}

			if !hasFunctionCalls(result.Output) {
				// Finished with agent loop, rebuild the PDF
				buildRun++
				err = workflow.ExecuteChildWorkflow(
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
				).Get(ctx, &pdfUrl)
				if err != nil {
					var appErr *temporal.ApplicationError
					if errors.As(err, &appErr) && appErr.Type() == activities.ErrTypeBuildFailed {
						// Build failed, so kick back to Ai to fix
						var details []string
						_ = appErr.Details(&details)
						pendingInput = responses.ResponseInputParam{userMessage(fmt.Sprintf(
							"Build failed, fix and try again: \n%s",
							strings.Join(details, "\n"),
						))}
						continue
					}
					return err
				}
				break
			}

			pendingInput = tools.ProcessToolCalls(ctx, filterFunctionCalls(result.Output), dispatcher)
		}

		// Update URL in PR description
		err = updatePRDescription(ctx, args.Repo, args.Pr, pdfUrl)
		if err != nil {
			return err
		}
	}

	// Mark PR as finished
	return workflow.ExecuteActivity(ctx, activities.FinishReview, args.Pr).Get(ctx, nil)
}

type reviewAgentDispatcher struct {
	aiTools     []responses.ToolUnionParam
	ghOpts      github.ClientOptions
	branchName  string
	buildTarget BuildTarget
}

func (d *reviewAgentDispatcher) Dispatch(ctx workflow.Context, call responses.ResponseOutputItemUnion) (workflow.Future, error) {
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
			Builder:       "typst", // TODO: remove this hardcoded builder
			File:          file,
		}
		return workflow.ExecuteActivity(ctx, activities.Build, req), nil
	case tools.ReviewPDFLayoutToolDesc.OfFunction.Name:
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

func availableReviewTools(aiTools []responses.ToolUnionParam, enableLayoutReview bool) []responses.ToolUnionParam {
	ret := append([]responses.ToolUnionParam{}, aiTools...)
	ret = append(ret, tools.BuildToolDesc)
	if enableLayoutReview {
		ret = append(ret, tools.ReviewPDFLayoutToolDesc)
	}
	return ret
}

func updatePRDescription(ctx workflow.Context, repo github.ClientOptions, pr int, url string) error {
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

	// Find existing PDF URL via prefix
	var oldUrl string
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prArtifactLinePrefix) {
			oldUrl = strings.TrimSpace(line[len(prArtifactLinePrefix):])
			lines[i] = prArtifactLinePrefix + " " + url
			break
		}
	}
	if oldUrl == "" {
		workflow.GetLogger(ctx).Warn("No existing artifact URL found in PR description")
		lines = append(lines, prArtifactLinePrefix+" "+url)
	}
	body = strings.Join(lines, "\n")

	// Delete old PDF URL from bucket
	err = workflow.ExecuteActivity(
		ctx,
		activities.DeletePDFByURL,
		activities.DeletePDFByURLRequest{URL: oldUrl},
	).Get(ctx, nil)
	if err != nil {
		return err
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
