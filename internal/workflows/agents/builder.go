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

func BuilderWorkflow(ctx workflow.Context, req BuilderAgentRequest) (int, error) {
	instructions, ok := buildTargetMap[req.BuildTarget]
	if !ok {
		return 0, fmt.Errorf("invalid build target: %d", req.BuildTarget)
	}

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(instructions),
		openai.UserMessage("Remote: " + req.Owner + "/" + req.Repo),
		openai.UserMessage("Branch Name: " + req.BranchName),
		openai.UserMessage("Job Application:\n" + req.Job),
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

	dispatcher := &builderDispatcher{
		aiTools:    aiTools,
		ghOpts:     req.ClientOptions,
		branchName: req.BranchName,
		builder:    req.Builder,
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
		err = workflow.ExecuteChildWorkflow(
			ctx,
			PullRequestAgent,
			PullRequestAgentRequest{
				ClientOptions: req.ClientOptions,
				Branch:        req.BranchName,
				Target:        req.TargetBranch,
				Job:           req.Job,
			},
		).Get(ctx, &prNum)
		if err != nil {
			return 0, err
		}

		return prNum, nil
	}
}

type builderDispatcher struct {
	aiTools    []openai.ChatCompletionToolUnionParam
	ghOpts     github.ClientOptions
	branchName string
	builder    Builder
}

func (d *builderDispatcher) Dispatch(ctx workflow.Context, call openai.ChatCompletionMessageToolCallUnion) (workflow.Future, error) {
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
			Builder:       d.builder,
		}
		return workflow.ExecuteActivity(ctx, activities.Build, req), nil
	default:
		return nil, fmt.Errorf("unsupported tool: %s", call.Function.Name)
	}
}

var buildTargetMap = map[BuildTarget]string{
	BuildTargetResume:      ResumeBuilderInstructions,
	BuildTargetCoverLetter: CoverLetterBuilderInstructions,
}

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

const CoverLetterBuilderInstructions = `
TODO
`
