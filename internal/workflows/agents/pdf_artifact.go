package agents

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/workflow"

	"github.com/ansg191/job-temporal/internal/activities"
	"github.com/ansg191/job-temporal/internal/github"
)

type BuildAndUploadPDFWorkflowRequest struct {
	github.ClientOptions
	Branch      string      `json:"branch"`
	Builder     string      `json:"builder"`
	BuildTarget BuildTarget `json:"build_target"`
}

func BuildAndUploadPDFWorkflow(ctx workflow.Context, req BuildAndUploadPDFWorkflowRequest) (string, error) {
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute * 3,
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	file, err := resolveBuildTargetFile(req.BuildTarget)
	if err != nil {
		return "", err
	}

	var artifactURL string
	var pdfContent []byte
	err = workflow.ExecuteActivity(ctx, activities.BuildFinalPDF, activities.BuildFinalPDFRequest{
		ClientOptions: req.ClientOptions,
		Branch:        req.Branch,
		Builder:       req.Builder,
		File:          file,
	}).Get(ctx, &pdfContent)
	if err != nil {
		return "", err
	}

	err = workflow.ExecuteActivity(ctx, activities.UploadPDF, activities.UploadPDFRequest{
		Content: pdfContent,
	}).Get(ctx, &artifactURL)
	if err != nil {
		return "", err
	}

	return artifactURL, nil
}

func resolveBuildTargetFile(buildTarget BuildTarget) (string, error) {
	switch buildTarget {
	case BuildTargetResume:
		return "resume.typ", nil
	case BuildTargetCoverLetter:
		return "cover_letter.typ", nil
	default:
		return "", fmt.Errorf("invalid build target: %d", buildTarget)
	}
}
