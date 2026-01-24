package webhook

import (
	"context"
	"fmt"
	"time"

	"github.com/ansg191/job-temporal/internal/database"
)

const SignalName = "github-webhook"
const ReviewAgentSignal = "review-agent-signal"

type WebhookSignal struct {
	Type        string // "pull_request_review", "issue_comment", "pull_request_review_comment"
	Action      string // "submitted", "created", etc.
	Owner       string
	Repo        string
	PRNumber    int
	PRTitle     string
	PRBranch    string
	Body        string
	AuthorLogin string
	ReviewState string // For reviews: "approved", "changes_requested", "commented"
	Timestamp   time.Time

	// Location fields for review comments
	FilePath  string // Path to the file being commented on
	Line      int    // Line number in the file
	StartLine int    // Start line for multi-line comments (0 if single line)
	DiffHunk  string // Diff context around the comment
}

// WorkflowIDResolver resolves the workflow ID for a given PR.
type WorkflowIDResolver interface {
	Resolve(ctx context.Context, owner, repo string, prNumber int) (string, error)
}

// PRBasedResolver is a placeholder implementation that generates workflow IDs based on PR info.
// TODO: Replace with PostgresResolver that queries workflow_id from PR number.
type PRBasedResolver struct{}

func (r *PRBasedResolver) Resolve(ctx context.Context, owner, repo string, prNumber int) (string, error) {
	return fmt.Sprintf("pr-%s-%s-%d", owner, repo, prNumber), nil
}

type PostgresResolver struct {
	db database.Database
}

func (p *PostgresResolver) Resolve(ctx context.Context, owner, repo string, prNumber int) (string, error) {
	_, _ = repo, owner
	return p.db.GetPrWorkflowId(ctx, prNumber)
}
