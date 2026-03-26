package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
	// AddMemory inserts a new memory entry scoped to owner/repo. Returns the new entry's ID.
	AddMemory(ctx context.Context, owner, repo, content string) (int, error)
	// ListMemories returns the most recent memories for a repo, ordered oldest-first.
	ListMemories(ctx context.Context, owner, repo string, limit int) ([]MemoryEntry, error)
	// DeleteMemory removes a memory entry by ID, scoped to owner/repo. Returns true if a row was deleted.
	// The owner/repo check prevents cross-repo deletion.
	DeleteMemory(ctx context.Context, owner, repo string, id int) (bool, error)
}

type JobRun struct {
	WorkflowID      string
	SourceURL       string
	ScrapedMarkdown string
	BranchName      string
	CreatedAt       time.Time
}

type MemoryEntry struct {
	ID        int
	Content   string
	CreatedAt time.Time
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

func (p *postgresDatabase) AddMemory(ctx context.Context, owner, repo, content string) (int, error) {
	var id int
	err := p.db.QueryRowContext(ctx,
		"INSERT INTO agent_memory (owner, repo, content) VALUES ($1, $2, $3) RETURNING id",
		owner, repo, content).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("add memory: %w", err)
	}
	return id, nil
}

func (p *postgresDatabase) ListMemories(ctx context.Context, owner, repo string, limit int) ([]MemoryEntry, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := p.db.QueryContext(ctx,
		`SELECT id, content, created_at FROM (
			SELECT id, content, created_at FROM agent_memory
			WHERE owner = $1 AND repo = $2
			ORDER BY created_at DESC LIMIT $3
		) sub ORDER BY created_at ASC`,
		owner, repo, limit)
	if err != nil {
		return nil, fmt.Errorf("list memories: %w", err)
	}
	defer rows.Close()

	entries := make([]MemoryEntry, 0, limit)
	for rows.Next() {
		var entry MemoryEntry
		if err := rows.Scan(&entry.ID, &entry.Content, &entry.CreatedAt); err != nil {
			return nil, fmt.Errorf("list memories scan: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list memories rows: %w", err)
	}

	return entries, nil
}

func (p *postgresDatabase) DeleteMemory(ctx context.Context, owner, repo string, id int) (bool, error) {
	result, err := p.db.ExecContext(ctx,
		"DELETE FROM agent_memory WHERE id = $1 AND owner = $2 AND repo = $3",
		id, owner, repo)
	if err != nil {
		return false, fmt.Errorf("delete memory: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("delete memory rows affected: %w", err)
	}
	return affected == 1, nil
}

func getDBUrl() string {
	return os.Getenv("DATABASE_URL")
}
