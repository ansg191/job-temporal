package workflows

import (
	"encoding/json"
	"time"

	"github.com/openai/openai-go/v3"
	"go.temporal.io/sdk/workflow"

	"github.com/ansg191/job-temporal/internal/activities"
	"github.com/ansg191/job-temporal/internal/tools"
	"github.com/ansg191/job-temporal/internal/workflows/agents"
)

func AgentWorkflow(ctx workflow.Context, remote, input string) (string, error) {
	messages := []openai.ChatCompletionMessageParamUnion{
		//openai.SystemMessage("You are a helpful assistant. " +
		//	"Use read_file to read a file. Use edit_file to edit a file. Do not call edit_file unnecessarily (if no change required)." +
		//	"Call build tool to compile the resume to check for errors. Always build and check for errors"),
		//openai.UserMessage(input),
		openai.SystemMessage(agents.ResumeBuilderInstructions),
		openai.UserMessage("Job Application:\n" + input),
	}

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: time.Second * 10,
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
				Tools: []openai.ChatCompletionToolUnionParam{
					tools.ReadFileToolDesc,
					tools.EditFileToolDesc,
					tools.EditLineToolDesc,
					tools.BuildToolDesc,
				},
			},
		).Get(ctx, &result)
		if err != nil {
			return "", err
		}

		js, _ := json.Marshal(result)
		workflow.GetLogger(ctx).Info("AI response", "result", string(js))

		messages = append(messages, result.Choices[0].Message.ToParam())

		if result.Choices[0].FinishReason == "tool_calls" {
			futs := make([]workflow.Future, len(result.Choices[0].Message.ToolCalls))
			for i, call := range result.Choices[0].Message.ToolCalls {
				name := call.Function.Name
				args := call.Function.Arguments

				switch name {
				case tools.ReadFileToolDesc.OfFunction.Function.Name:
					req := activities.ReadFileRequest{
						AllowList:  []string{"person.typ", "projects.typ", "jobs.typ", "school.typ", "resume.typ"},
						RepoRemote: remote,
						Branch:     "abc",
					}
					err = tools.ReadToolParseArgs(args, &req)
					if err != nil {
						messages = append(messages, openai.ToolMessage(err.Error(), call.ID))
						continue
					}

					futs[i] = workflow.ExecuteActivity(ctx, activities.ReadFile, req)
				case tools.EditFileToolDesc.OfFunction.Function.Name:
					req := activities.EditFileRequest{
						ReadFileRequest: activities.ReadFileRequest{
							AllowList:  []string{"person.typ", "projects.typ", "jobs.typ", "school.typ"},
							RepoRemote: remote,
							Branch:     "abc",
						},
					}
					err = tools.EditToolParseArgs(args, &req)
					if err != nil {
						messages = append(messages, openai.ToolMessage(err.Error(), call.ID))
						continue
					}

					futs[i] = workflow.ExecuteActivity(ctx, activities.EditFile, req)
				case tools.EditLineToolDesc.OfFunction.Function.Name:
					req := activities.EditLineRequest{
						ReadFileRequest: activities.ReadFileRequest{
							AllowList:  []string{"person.typ", "projects.typ", "jobs.typ", "school.typ"},
							RepoRemote: remote,
							Branch:     "abc",
						},
					}
					err = tools.EditLineToolParseArgs(args, &req)
					if err != nil {
						messages = append(messages, openai.ToolMessage(err.Error(), call.ID))
						continue
					}

					futs[i] = workflow.ExecuteActivity(ctx, activities.EditLine, req)
				case tools.BuildToolDesc.OfFunction.Function.Name:
					req := activities.BuildRequest{
						RepoRemote: remote,
						Branch:     "abc",
						Builder:    "typst",
					}
					futs[i] = workflow.ExecuteActivity(ctx, activities.Build, req)
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
			return result.Choices[0].Message.Content, nil
		}
	}
	//return result.Choices[0].Message.Content, nil
}
