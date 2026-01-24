package agents

import (
	"slices"
	"time"

	"github.com/openai/openai-go/v3"
	"go.temporal.io/sdk/workflow"

	"github.com/ansg191/job-temporal/internal/activities"
	"github.com/ansg191/job-temporal/internal/github"
	"github.com/ansg191/job-temporal/internal/tools"
)

const ResumeBuilderInstructions = `
You are a resume builder who creates personalized resumes for applicants that
are specialized for specific applications.

CORE RESPONSIBILITIES:
1. Read applicant's profile from the resume pages and identify relevant points for the target job.
2. Tailor the resume to highlight skills and experiences that match the job description.
3. Ensure the resume is well-structured, clear, and professional.
4. Use action verbs and quantify achievements where possible.
5. Avoid unnecessary punctuation (parenthesis, semicolons, dashes, etc)

IMPORTANT NOTES:
- The Resume is built in typst. You will edit typst files that are used by a
template to build the resume.
- Important pages:
	- person.typ: Information about the applicant
	- jobs.typ: Information about professional experience
	- school.typ: Information about educational background
	- projects.typ: Information about personal projects
	- resume.typ: Resume formatting file that pulls info from other files. You can read this file for context, but do NOT edit it.
- The Resume MUST be under 1 page. This will be checked by the build tool.
- Only work in in the repository provided
- Only work in the branch provided

AVAILABLE TOOLS:
- Github MCP tools to read and edit files in the applicant's resume repository.
- build(): Compile the resume and perform various checks
`

func ResumeBuilderWorkflow(ctx workflow.Context, owner, repo, branchName, input string) (int, error) {
	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(ResumeBuilderInstructions),
		openai.UserMessage("Remote: " + owner + "/" + repo),
		openai.UserMessage("Branch Name: " + branchName),
		openai.UserMessage("Job Application:\n" + input),
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

	for {
		var result *openai.ChatCompletion
		err := workflow.ExecuteActivity(
			ctx,
			activities.CallAI,
			activities.OpenAIResponsesRequest{
				Model:    openai.ChatModelGPT5_2,
				Messages: messages,
				Tools:    append(aiTools, tools.BuildToolDesc),
			},
		).Get(ctx, &result)
		if err != nil {
			return 0, err
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
						ClientOptions: github.ClientOptions{Owner: owner, Repo: repo},
						Branch:        branchName,
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
			// Activate PR Builder workflow
			var prNum int
			err := workflow.ExecuteChildWorkflow(ctx, PullRequestAgent, owner, repo, branchName, input).Get(ctx, &prNum)
			if err != nil {
				return 0, err
			}

			return prNum, nil
		}
	}
}
