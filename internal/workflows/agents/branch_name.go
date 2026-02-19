package agents

import (
	"fmt"
	"time"

	"github.com/openai/openai-go/v3/responses"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/ansg191/job-temporal/internal/activities"
	"github.com/ansg191/job-temporal/internal/github"
	"github.com/ansg191/job-temporal/internal/tools"
)

type BranchNameAgentPurpose string

const (
	BranchNameAgentPurposeResume      BranchNameAgentPurpose = "resume"
	BranchNameAgentPurposeCoverLetter BranchNameAgentPurpose = "cover_letter"
	BranchNameAgentPurposeFinal       BranchNameAgentPurpose = "final"
)

type BranchNameAgentRequest struct {
	github.ClientOptions
	JobDescription string                 `json:"job_description"`
	Purpose        BranchNameAgentPurpose `json:"purpose"`
}

func BranchNameAgent(ctx workflow.Context, req BranchNameAgentRequest) (string, error) {
	if req.Purpose != BranchNameAgentPurposeResume &&
		req.Purpose != BranchNameAgentPurposeCoverLetter &&
		req.Purpose != BranchNameAgentPurposeFinal {
		return "", temporal.NewNonRetryableApplicationError("invalid purpose", "InvalidPurpose", nil)
	}

	agentCfg, err := loadAgentConfig(ctx, "branch_name")
	if err != nil {
		return "", err
	}

	messages := responses.ResponseInputParam{
		systemMessage(agentCfg.Instructions),
		userMessage("Purpose: " + string(req.Purpose)),
		userMessage("Job Description:\n" + req.JobDescription),
	}

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	dispatcher := &branchNameDispatcher{ghOpts: req.ClientOptions}
	conversationID, err := createConversation(ctx, nil)
	if err != nil {
		return "", err
	}
	callAICtx := withCallAIActivityOptions(ctx)

	for range 5 {
		var result *responses.Response
		err = workflow.ExecuteActivity(
			callAICtx,
			activities.CallAI,
			activities.OpenAIResponsesRequest{
				Model: agentCfg.Model,
				Input: messages,
				Tools: []responses.ToolUnionParam{
					tools.ListBranchesToolDesc,
				},
				Temperature:    temperatureOpt(agentCfg.Temperature),
				ConversationID: conversationID,
			},
		).Get(ctx, &result)
		if err != nil {
			return "", err
		}

		if hasFunctionCalls(result.Output) {
			messages = tools.ProcessToolCalls(ctx, filterFunctionCalls(result.Output), dispatcher)
			continue
		}

		branchName := result.OutputText()

		req := activities.CreateBranchRequest{
			ClientOptions: req.ClientOptions,
			Branch:        branchName,
		}
		err = workflow.ExecuteActivity(ctx, activities.CreateBranch, req).Get(ctx, nil)
		if err != nil {
			messages = responses.ResponseInputParam{
				userMessage("Unable to create branch: " + err.Error() + "\n"),
			}
			continue
		}
		return branchName, nil
	}

	return "", temporal.NewNonRetryableApplicationError("failed to generate branch name", "BranchNameError", nil)
}

type branchNameDispatcher struct {
	ghOpts github.ClientOptions
}

func (d *branchNameDispatcher) Dispatch(ctx workflow.Context, call responses.ResponseOutputItemUnion) (workflow.Future, error) {
	switch call.Name {
	case tools.ListBranchesToolDesc.OfFunction.Name:
		req := activities.ListBranchesRequest{ClientOptions: d.ghOpts}
		return workflow.ExecuteActivity(ctx, activities.ListBranches, req), nil
	default:
		return nil, fmt.Errorf("unsupported tool: %s", call.Name)
	}
}
