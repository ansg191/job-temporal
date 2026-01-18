package workflows

import (
	"encoding/json"
	"time"

	"github.com/openai/openai-go/v3"
	"go.temporal.io/sdk/workflow"

	"github.com/ansg191/job-temporal/internal/activities"
	"github.com/ansg191/job-temporal/internal/tools"
)

func AgentWorkflow(ctx workflow.Context, remote, input string) (string, error) {
	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("You are a helpful assistant. " +
			"Use read_file to read a file. Use edit_file to edit a file. Do not call edit_file unnecessarily (if no change required)."),
		openai.UserMessage(input),
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

				if name == tools.ReadFileToolDesc.OfFunction.Function.Name {
					req := activities.ReadFileRequest{
						AllowList:  []string{"person.typ", "projects.typ"},
						RepoRemote: remote,
						Branch:     "abc",
					}
					err = tools.ReadToolParseArgs(args, &req)
					if err != nil {
						messages = append(messages, openai.ToolMessage(err.Error(), call.ID))
						continue
					}

					futs[i] = workflow.ExecuteActivity(ctx, activities.ReadFile, req)
				} else if name == tools.EditFileToolDesc.OfFunction.Function.Name {
					req := activities.EditFileRequest{
						ReadFileRequest: activities.ReadFileRequest{
							AllowList:  []string{"person.typ", "projects.typ"},
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
