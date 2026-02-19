package activities

import (
	"context"

	"github.com/ansg191/job-temporal/internal/database"
)

func RegisterReviewReadyPR(ctx context.Context, id, owner, repo, branch string, pr int) error {
	db, err := database.NewPostgresDatabase()
	if err != nil {
		return err
	}
	defer db.Close()

	return db.RegisterReviewReadyPR(ctx, id, owner, repo, branch, pr)
}

func FinishReview(ctx context.Context, workflowID string) error {
	db, err := database.NewPostgresDatabase()
	if err != nil {
		return err
	}
	defer db.Close()

	return db.FinishReviewWorkflow(ctx, workflowID)
}

type CreateJobRunRequest struct {
	WorkflowID      string
	SourceURL       string
	ScrapedMarkdown string
}

func CreateJobRun(ctx context.Context, req CreateJobRunRequest) error {
	db, err := database.NewPostgresDatabase()
	if err != nil {
		return err
	}
	defer db.Close()

	return db.CreateJobRun(ctx, req.WorkflowID, req.SourceURL, req.ScrapedMarkdown)
}

type UpdateJobRunBranchRequest struct {
	WorkflowID string
	BranchName string
}

func UpdateJobRunBranch(ctx context.Context, req UpdateJobRunBranchRequest) error {
	db, err := database.NewPostgresDatabase()
	if err != nil {
		return err
	}
	defer db.Close()

	return db.UpdateJobRunBranch(ctx, req.WorkflowID, req.BranchName)
}
