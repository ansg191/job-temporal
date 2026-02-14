package agents

import (
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
	instructions, ok := buildTargetMap[req.BuildTarget]
	if !ok {
		return 0, fmt.Errorf("invalid build target: %d", req.BuildTarget)
	}

	messages := responses.ResponseInputParam{
		systemMessage(instructions),
		userMessage("Remote: " + req.Owner + "/" + req.Repo),
		userMessage("Branch Name: " + req.BranchName),
		userMessage("Job Application:\n" + req.Job),
	}

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: time.Second * 30,
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var aiTools []responses.ToolUnionParam
	err := workflow.ExecuteActivity(ctx, activities.ListGithubTools).Get(ctx, &aiTools)
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
				Model:          openai.ChatModelGPT5_2,
				Input:          messages,
				Tools:          availableBuilderTools(aiTools),
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
5. Avoid unnecessary punctuation (parenthesis, semicolons, dashes, etc) except for common phrases
6. Make sure to bold relevant keywords in the resume using typst's #strong[] function

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
- After you are done, a review workflow will take your notes & run to ensure the resume is well-formatted.
- Always fix all high severity issues. Reduce medium issues as much as practical.
- If the issue CANNOT be solved without changing formatting (editing resume.typ), you MUST ignore it and output
a note explaining why for the review workflow.
- Do not finish or open a PR if high issues remain.

OUTPUT FORMAT:
Notes for the review workflow. Should be blank initially.
When you fix an issue, explain how you fixed it in the notes.
If you purposefully ignored issues, note them and explain why.
These notes should be cumulative, including notes from all previous reviews.

AVAILABLE TOOLS:
- Github MCP tools to read and edit files in the applicant's resume repository.
- build(): Compile the resume and perform various checks
`

const CoverLetterBuilderInstructions = `
You are a cover letter builder who creates personalized cover letters for applicants that
are specialized for specific applications.

CORE RESPONSIBILITIES:
1. Read applicant's profile from the cover letter pages and identify relevant points for the target job.
2. Tailor the cover letter to highlight skills and experiences that match the job description.
3. Ensure the cover letter is well-structured, clear, and professional.
4. Avoid unnecessary punctuation (parenthesis, semicolons, dashes, etc) except for common phrases

IMPORTANT NOTES:
- The Cover Letter is built in typst. You will edit typst files that are used by a
template to build the cover letter.
- Important pages:
	- person.typ: Information about the applicant
	- jobs.typ: Information about professional experience
	- school.typ: Information about educational background
	- projects.typ: Information about personal projects
	- letter.typ: Cover letter content file
	- cover_letter.typ: Cover letter formatting file that pulls info from other files. You can read this file for context, but do NOT edit it.
- The Cover Letter MUST be under 1 page. This will be checked by the build tool.
- Only work in in the repository provided
- Only work in the branch provided

AVAILABLE TOOLS:
- Github MCP tools to read and edit files in the applicant's resume repository.
- build(): Compile the cover letter and perform various checks
`
