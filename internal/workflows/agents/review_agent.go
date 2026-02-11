package agents

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
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

AVAILABLE TOOLS:
- Github MCP tools to read and edit files in the applicant's resume repository.
- build(): Compile the resume and perform various checks
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

		signalBytes, err := json.Marshal(signal)
		if err != nil {
			return err
		}
		workflow.GetLogger(ctx).Info("Received signal: " + string(signalBytes))
		messages = append(
			messages,
			userMessage("User Review: "+string(signalBytes)),
		)

		dispatcher := &reviewAgentDispatcher{
			aiTools:     aiTools,
			ghOpts:      args.Repo,
			branchName:  args.BranchName,
			buildTarget: args.BuildTarget,
		}

		for {
			var result *responses.Response
			err = workflow.ExecuteActivity(
				ctx,
				activities.CallAI,
				activities.OpenAIResponsesRequest{
					Model: openai.ChatModelGPT5_2,
					Input: messages,
					Tools: append(aiTools, tools.BuildToolDesc),
				},
			).Get(ctx, &result)
			if err != nil {
				return err
			}

			messages = appendOutput(messages, result.Output)

			if !hasFunctionCalls(result.Output) {
				break
			}

			toolMsgs := tools.ProcessToolCalls(ctx, filterFunctionCalls(result.Output), dispatcher)
			messages = append(messages, toolMsgs...)
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
		var file string
		switch d.buildTarget {
		case BuildTargetResume:
			file = "resume.typ"
		case BuildTargetCoverLetter:
			file = "cover_letter.typ"
		}

		req := activities.BuildRequest{
			ClientOptions: d.ghOpts,
			Branch:        d.branchName,
			Builder:       "typst",
			File:          file,
		}
		return workflow.ExecuteActivity(ctx, activities.Build, req), nil
	default:
		return nil, fmt.Errorf("unsupported tool: %s", call.Name)
	}
}
