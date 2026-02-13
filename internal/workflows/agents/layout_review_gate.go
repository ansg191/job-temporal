package agents

import (
	"encoding/json"
	"fmt"
	"strings"

	"go.temporal.io/sdk/workflow"

	"github.com/ansg191/job-temporal/internal/activities"
)

const (
	// Be strict initially, then relax to avoid endless loops when the editor is effectively blind.
	layoutReviewStrictMediumAttempts = 3
	// Even after relaxing, too many medium issues still blocks completion.
	layoutReviewMaxMediumAfterRelax = 2
)

func runLayoutReviewGate(
	ctx workflow.Context,
	childWorkflowID string,
	req activities.ReviewPDFLayoutRequest,
) (*activities.ReviewPDFLayoutOutput, string, error) {
	var reviewJSON string
	err := workflow.ExecuteChildWorkflow(
		workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			WorkflowID: childWorkflowID,
		}),
		ReviewPDFLayoutWorkflow,
		req,
	).Get(ctx, &reviewJSON)
	if err != nil {
		return nil, "", err
	}

	var output activities.ReviewPDFLayoutOutput
	if err = json.Unmarshal([]byte(reviewJSON), &output); err != nil {
		return nil, "", fmt.Errorf("failed to parse layout review output: %w", err)
	}
	return &output, reviewJSON, nil
}

func shouldBlockLayoutIssues(output *activities.ReviewPDFLayoutOutput, attempt int) (bool, string) {
	if output == nil {
		return false, ""
	}

	high := 0
	medium := 0
	for _, issue := range output.Issues {
		switch strings.ToLower(strings.TrimSpace(issue.Severity)) {
		case "high":
			high++
		case "medium":
			medium++
		}
	}

	if high > 0 {
		return true, "high severity issues remain"
	}
	if medium == 0 {
		return false, ""
	}
	if attempt <= layoutReviewStrictMediumAttempts {
		return true, "medium severity issues remain during strict pass"
	}
	if medium > layoutReviewMaxMediumAfterRelax {
		return true, "too many medium severity issues remain"
	}
	return false, ""
}
