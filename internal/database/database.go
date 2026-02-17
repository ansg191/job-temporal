package database

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"

	_ "github.com/lib/pq"
)

var ErrNotFound = errors.New("not found")

type Database interface {
	io.Closer
	// RegisterReviewReadyPR links a pull request to a workflow, marking it as ready for review in the system.
	RegisterReviewReadyPR(ctx context.Context, workflowId string, prNumber int) error
	// GetPrWorkflowId returns the workflow ID for a given pull request number.
	// Will return ErrNotFound if the PR is not registered or PR is already finished.
	GetPrWorkflowId(ctx context.Context, prNumber int) (string, error)
	// FinishPR marks a pull request as finished in the system.
	FinishPR(ctx context.Context, prNumber int) error
	// CreateJobRun inserts a new job run record.
	CreateJobRun(ctx context.Context, workflowID, sourceURL, scrapedMarkdown string) error
	// UpdateJobRunBranch sets the final branch name for a job run.
	UpdateJobRunBranch(ctx context.Context, workflowID, branchName string) error
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

func (p *postgresDatabase) RegisterReviewReadyPR(ctx context.Context, workflowId string, prNumber int) error {
	_, err := p.db.ExecContext(ctx,
		"INSERT INTO workflows (workflow_id, pull_request_id, finished) VALUES ($1, $2, false)",
		workflowId, prNumber)
	return err
}

func (p *postgresDatabase) GetPrWorkflowId(ctx context.Context, prNumber int) (string, error) {
	var workflowId string
	err := p.db.QueryRowContext(ctx,
		"SELECT workflow_id FROM workflows WHERE pull_request_id = $1 AND finished = false",
		prNumber).Scan(&workflowId)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return workflowId, nil
}

func (p *postgresDatabase) FinishPR(ctx context.Context, prNumber int) error {
	_, err := p.db.ExecContext(ctx,
		"UPDATE workflows SET finished = true WHERE pull_request_id = $1",
		prNumber)
	return err
}

func (p *postgresDatabase) CreateJobRun(ctx context.Context, workflowID, sourceURL, scrapedMarkdown string) error {
	_, err := p.db.ExecContext(ctx,
		"INSERT INTO job_runs (workflow_id, source_url, scraped_markdown) VALUES ($1, $2, $3)",
		workflowID, sourceURL, scrapedMarkdown)
	return err
}

func (p *postgresDatabase) UpdateJobRunBranch(ctx context.Context, workflowID, branchName string) error {
	_, err := p.db.ExecContext(ctx,
		"UPDATE job_runs SET branch_name = $1 WHERE workflow_id = $2",
		branchName, workflowID)
	return err
}

func getDBUrl() string {
	return os.Getenv("DATABASE_URL")
}
