package workflows

import (
	"time"

	"go.temporal.io/sdk/workflow"

	"github.com/ansg191/job-temporal/internal/activities"
)

func SayHelloWorkflow(ctx workflow.Context, name string) (string, error) {
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: time.Second * 10,
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var result string
	err := workflow.ExecuteActivity(ctx, activities.Greet, name).Get(ctx, &result)
	if err != nil {
		return "", err
	}

	workflow.GetLogger(ctx).Info("Workflow completed.", "result", result)

	return result, nil
}
