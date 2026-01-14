package workflows

import (
	"encoding/json"
	"time"

	"github.com/openai/openai-go/v3"
	"go.temporal.io/sdk/workflow"

	"github.com/ansg191/job-temporal/internal/activities"
	"github.com/ansg191/job-temporal/internal/tools"
)

func AgentWorkflow(ctx workflow.Context, input string) (string, error) {
	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("Say Hello to this person. Use the get_name tool to get the name of the person."),
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
					tools.GetNameToolDesc,
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
			toolCall := result.Choices[0].Message.ToolCalls[0].Function.Name
			if toolCall == "get_name" {
				var toolRes *tools.ToolActivityResult
				err = workflow.ExecuteActivity(ctx, activities.ToolGetName, nil).Get(ctx, &toolRes)
				if err != nil {
					return "", err
				}
				if toolRes.Success {
					messages = append(messages, openai.ToolMessage(toolRes.Result, result.Choices[0].Message.ToolCalls[0].ID))
				} else {
					messages = append(messages, openai.ToolMessage(toolRes.Error, result.Choices[0].Message.ToolCalls[0].ID))
				}
			}
		} else {
			return result.Choices[0].Message.Content, nil
		}
	}
	//return result.Choices[0].Message.Content, nil
}
