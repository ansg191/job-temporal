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
		userMessage("Remote: " + req.Owner + "/" + req.Repo),
		userMessage("Branch Name: " + req.BranchName),
		userMessage("Job Application:\n" + req.Job),
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

// Deprecated: buildTargetMap is kept for rollback safety. Use GetAgentConfig with buildTargetAgentName instead.
var buildTargetMap = map[BuildTarget]string{
	BuildTargetResume:      ResumeBuilderInstructions,
	BuildTargetCoverLetter: CoverLetterBuilderInstructions,
}

// Deprecated: ResumeBuilderInstructions is kept for rollback safety. Use GetAgentConfig("builder_resume") instead.
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

// Deprecated: CoverLetterBuilderInstructions is kept for rollback safety. Use GetAgentConfig("builder_cover_letter") instead.
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
