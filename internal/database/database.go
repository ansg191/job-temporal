package database

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"time"

	_ "github.com/lib/pq"
)

var ErrNotFound = errors.New("not found")

type Database interface {
	io.Closer
	// RegisterReviewReadyPR links a pull request to a workflow, marking it as ready for review in the system.
	RegisterReviewReadyPR(ctx context.Context, workflowID, owner, repo, branchName string, prNumber int) error
	// GetPrWorkflowId returns the workflow ID for a given pull request number.
	// Will return ErrNotFound if the PR is not registered or PR is already finished.
	GetPrWorkflowId(ctx context.Context, owner, repo string, prNumber int) (string, error)
	// GetBranchWorkflowId returns the workflow ID for a given branch.
	// Will return ErrNotFound if the branch is not registered or is already finished.
	GetBranchWorkflowId(ctx context.Context, owner, repo, branchName string) (string, error)
	// FinishReviewWorkflow marks a review workflow as finished in the system.
	FinishReviewWorkflow(ctx context.Context, workflowID string) error
	// CreateJobRun inserts a new job run record.
	CreateJobRun(ctx context.Context, workflowID, sourceURL, scrapedMarkdown string) error
	// UpdateJobRunBranch sets the final branch name for a job run.
	UpdateJobRunBranch(ctx context.Context, workflowID, branchName string) error
	// ListJobRuns returns recent job runs ordered by creation time descending.
	ListJobRuns(ctx context.Context, limit int) ([]JobRun, error)
}

type JobRun struct {
	WorkflowID      string
	SourceURL       string
	ScrapedMarkdown string
	BranchName      string
	CreatedAt       time.Time
}

type postgresDatabase struct {
	db *sql.DB
}

func (p *postgresDatabase) Close() error {
	return p.db.Close()
}

func NewPostgresDatabase() (Database, error) {
	db, err := sql.Open("postgres", getDBUrl())
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	return &postgresDatabase{db: db}, nil
}

func (p *postgresDatabase) RegisterReviewReadyPR(ctx context.Context, workflowID, owner, repo, branchName string, prNumber int) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO workflows (workflow_id, owner, repo, branch_name, pull_request_id, finished)
VALUES ($1, $2, $3, $4, $5, false)
ON CONFLICT (workflow_id) DO UPDATE SET
	owner = EXCLUDED.owner,
	repo = EXCLUDED.repo,
	branch_name = EXCLUDED.branch_name,
	pull_request_id = EXCLUDED.pull_request_id,
	finished = false`,
		workflowID, owner, repo, branchName, prNumber)
	return err
}

func (p *postgresDatabase) GetPrWorkflowId(ctx context.Context, owner, repo string, prNumber int) (string, error) {
	var workflowId string
	err := p.db.QueryRowContext(ctx,
		`SELECT workflow_id
FROM workflows
WHERE pull_request_id = $3
  AND finished = false
  AND (
    (owner = $1 AND repo = $2)
    OR (owner = '' AND repo = '')
  )
ORDER BY
  CASE
    WHEN owner = $1 AND repo = $2 THEN 0
    ELSE 1
  END
LIMIT 1;`,
		owner, repo, prNumber).Scan(&workflowId)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return workflowId, nil
}

func (p *postgresDatabase) GetBranchWorkflowId(ctx context.Context, owner, repo, branchName string) (string, error) {
	var workflowId string
	err := p.db.QueryRowContext(ctx,
		"SELECT workflow_id FROM workflows WHERE owner = $1 AND repo = $2 AND branch_name = $3 AND finished = false",
		owner, repo, branchName).Scan(&workflowId)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return workflowId, nil
}

func (p *postgresDatabase) FinishReviewWorkflow(ctx context.Context, workflowID string) error {
	_, err := p.db.ExecContext(ctx,
		"UPDATE workflows SET finished = true WHERE workflow_id = $1",
		workflowID)
	return err
}

func (p *postgresDatabase) CreateJobRun(ctx context.Context, workflowID, sourceURL, scrapedMarkdown string) error {
	_, err := p.db.ExecContext(ctx,
		"INSERT INTO job_runs (workflow_id, source_url, scraped_markdown) VALUES ($1, $2, $3) "+
			"ON CONFLICT (workflow_id) DO NOTHING",
		workflowID, sourceURL, scrapedMarkdown)
	return err
}

func (p *postgresDatabase) UpdateJobRunBranch(ctx context.Context, workflowID, branchName string) error {
	_, err := p.db.ExecContext(ctx,
		"UPDATE job_runs SET branch_name = $1 WHERE workflow_id = $2",
		branchName, workflowID)
	return err
}

func (p *postgresDatabase) ListJobRuns(ctx context.Context, limit int) ([]JobRun, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := p.db.QueryContext(ctx,
		"SELECT workflow_id, source_url, scraped_markdown, COALESCE(branch_name, ''), created_at "+
			"FROM job_runs ORDER BY created_at DESC LIMIT $1",
		limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	runs := make([]JobRun, 0, limit)
	for rows.Next() {
		var run JobRun
		if err := rows.Scan(&run.WorkflowID, &run.SourceURL, &run.ScrapedMarkdown, &run.BranchName, &run.CreatedAt); err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return runs, nil
}

func getDBUrl() string {
	return os.Getenv("DATABASE_URL")
}
