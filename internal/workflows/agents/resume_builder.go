package agents

import (
	"time"

	"github.com/openai/openai-go/v3"
	"go.temporal.io/sdk/workflow"

	"github.com/ansg191/job-temporal/internal/activities"
)

const ResumeBuilderInstructions = `
You are a resume builder who creates personalized resumes for applicants that
are specialized for specific applications.

CORE RESPONSIBILITIES:
1. Read applicant's profile from the resume pages and identify relevant points for the target job.
2. Tailor the resume to highlight skills and experiences that match the job description.
3. Ensure the resume is well-structured, clear, and professional.
4. Use action verbs and quantify achievements where possible.

IMPORTANT NOTES:
- The Resume is built in typst. You will edit typst files that are used by a
template to build the resume.
- Important pages:
	- person.typ: Information about the applicant
	- jobs.typ: Information about professional experience
	- school.typ: Information about educational background
	- projects.typ: Information about personal projects
	- resume.typ: Resume formatting file that pulls info from other files. You can read this file for context, but cannot edit it.
- The Resume MUST be under 1 page. This will be checked by the build tool.

AVAILABLE TOOLS:
- read_file(<file>): Read the contents of a file.
- edit_line(<file>, <start_line>, <end_line>, <new_content>, <message>): Edit lines in a file.
  - Line numbers are 1-indexed
  - To replace line 5: start_line=5, end_line=5
  - To replace lines 5-7: start_line=5, end_line=7
  - To insert before line 5: start_line=5, end_line=4
  - To delete lines: use empty new_content
- build(): Compile the resume and perform various checks
`

func ResumeBuilderWorkflow(ctx workflow.Context, job, applicant string) error {
	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(ResumeBuilderInstructions),
		openai.UserMessage("Job Application:\n" + job),
		openai.UserMessage("Applicant:\n" + applicant),
	}

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: time.Second * 30,
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	for {
		var result *openai.ChatCompletion
		err := workflow.ExecuteActivity(
			ctx,
			activities.CallAI,
			activities.OpenAIResponsesRequest{
				Model:    openai.ChatModelGPT5_2,
				Messages: messages,
				Tools:    nil,
			},
		).Get(ctx, &result)
		if err != nil {
			return err
		}

		messages = append(messages, result.Choices[0].Message.ToParam())

		if result.Choices[0].FinishReason == "tool_calls" {
			results, err := CallTools(ctx, result.Choices[0].Message.ToolCalls)
			if err != nil {
				return err
			}
			messages = append(messages, results...)
		} else {
			break
		}
	}

	return nil
}

func CallTools(
	ctx workflow.Context,
	call []openai.ChatCompletionMessageToolCallUnion,
) ([]openai.ChatCompletionMessageParamUnion, error) {
	return nil, nil
}
