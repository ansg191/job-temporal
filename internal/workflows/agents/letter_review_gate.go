package agents

import (
	"encoding/json"
	"fmt"

	"go.temporal.io/sdk/workflow"

	"github.com/ansg191/job-temporal/internal/activities"
)

func runLetterReviewGate(
	ctx workflow.Context,
	childWorkflowID string,
	req activities.ReviewLetterContentRequest,
) (*activities.ReviewLetterContentOutput, string, error) {
	var reviewJSON string
	err := workflow.ExecuteChildWorkflow(
		workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			WorkflowID: childWorkflowID,
		}),
		ReviewLetterContentWorkflow,
		req,
	).Get(ctx, &reviewJSON)
	if err != nil {
		return nil, "", err
	}

	var output activities.ReviewLetterContentOutput
	if err = json.Unmarshal([]byte(reviewJSON), &output); err != nil {
		return nil, "", fmt.Errorf("failed to parse letter review output: %w", err)
	}
	return &output, reviewJSON, nil
}

func shouldBlockLetterIssues(output *activities.ReviewLetterContentOutput, attempt int) (bool, string) {
	if output == nil {
		return false, ""
	}
	return shouldBlockReviewBySeverity(output.Issues, attempt)
}
