package agents

import (
	"fmt"
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

	dispatcher := &resumeBuilderDispatcher{
		aiTools:    aiTools,
		ghOpts:     github.ClientOptions{Owner: owner, Repo: repo},
		branchName: branchName,
	}

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
			return 0, err
		}

		messages = append(messages, result.Choices[0].Message.ToParam())

		if result.Choices[0].FinishReason == "tool_calls" {
			toolMsgs := tools.ProcessToolCalls(ctx, result.Choices[0].Message.ToolCalls, dispatcher)
			messages = append(messages, toolMsgs...)
			continue
		}

		// Activate PR Builder workflow
		var prNum int
		err = workflow.ExecuteChildWorkflow(ctx, PullRequestAgent, owner, repo, branchName, input).Get(ctx, &prNum)
		if err != nil {
			return 0, err
		}

		return prNum, nil
	}
}

type resumeBuilderDispatcher struct {
	aiTools    []openai.ChatCompletionToolUnionParam
	ghOpts     github.ClientOptions
	branchName string
}

func (d *resumeBuilderDispatcher) Dispatch(ctx workflow.Context, call openai.ChatCompletionMessageToolCallUnion) (workflow.Future, error) {
	if slices.ContainsFunc(d.aiTools, func(param openai.ChatCompletionToolUnionParam) bool {
		return param.GetFunction().Name == call.Function.Name
	}) {
		return workflow.ExecuteActivity(ctx, activities.CallGithubTool, call), nil
	}

	switch call.Function.Name {
	case tools.BuildToolDesc.OfFunction.Function.Name:
		req := activities.BuildRequest{
			ClientOptions: d.ghOpts,
			Branch:        d.branchName,
			Builder:       "typst",
		}
		return workflow.ExecuteActivity(ctx, activities.Build, req), nil
	default:
		return nil, fmt.Errorf("unsupported tool: %s", call.Function.Name)
	}
}
