package agents

import (
	"encoding/json"
	"slices"
	"strconv"
	"time"

	"github.com/openai/openai-go/v3"
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
	Repo       github.ClientOptions `json:"repo"`
	Pr         int                  `json:"pr"`
	BranchName string               `json:"branch_name"`
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

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(ReviewAgentInstructions),
		openai.UserMessage("Remote: " + args.Repo.Owner + "/" + args.Repo.Repo),
		openai.UserMessage("Pull Request: " + strconv.Itoa(args.Pr)),
		openai.UserMessage("Branch Name: " + args.BranchName),
	}

	var aiTools []openai.ChatCompletionToolUnionParam
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
			openai.UserMessage("User Review: "+string(signalBytes)),
		)

		for {
			var result *openai.ChatCompletion
			err = workflow.ExecuteActivity(
				ctx,
				activities.CallAI,
				activities.OpenAIResponsesRequest{
					Model:    openai.ChatModelGPT5_2,
					Messages: messages,
					Tools:    append(aiTools, tools.BuildToolDesc),
				},
			).Get(ctx, &result)
			if err != nil {
				return err
			}

			messages = append(messages, result.Choices[0].Message.ToParam())

			if result.Choices[0].FinishReason == "tool_calls" {
				futs := make([]workflow.Future, len(result.Choices[0].Message.ToolCalls))
				for i, call := range result.Choices[0].Message.ToolCalls {
					name := call.Function.Name
					//args := call.Function.Arguments

					if slices.ContainsFunc(aiTools, func(param openai.ChatCompletionToolUnionParam) bool {
						tName := param.GetFunction().Name
						return tName == name
					}) {
						futs[i] = workflow.ExecuteActivity(ctx, activities.CallGithubTool, call)
						continue
					}

					switch name {
					case tools.BuildToolDesc.OfFunction.Function.Name:
						req := activities.BuildRequest{
							ClientOptions: args.Repo,
							Branch:        args.BranchName,
							Builder:       "typst",
						}
						futs[i] = workflow.ExecuteActivity(ctx, activities.Build, req)
					default:
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
				break
			}
		}
	}

	// Mark PR as finished
	return workflow.ExecuteActivity(ctx, activities.FinishReview, args.Pr).Get(ctx, nil)
}
