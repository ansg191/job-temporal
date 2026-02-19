package webhook

import (
	"context"
	"fmt"
	"time"

	"github.com/ansg191/job-temporal/internal/database"
)

const SignalName = "github-webhook"
const ReviewAgentSignal = "review-agent-signal"
const RebuildSignal = "rebuild-signal"

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

// WorkflowIDResolver resolves the workflow ID for a given webhook signal.
type WorkflowIDResolver interface {
	Resolve(ctx context.Context, signal *WebhookSignal) (string, error)
}

// PRBasedResolver is a placeholder implementation that generates workflow IDs based on PR info.
type PRBasedResolver struct{}

func (r *PRBasedResolver) Resolve(_ context.Context, signal *WebhookSignal) (string, error) {
	return fmt.Sprintf("pr-%s-%s-%d", signal.Owner, signal.Repo, signal.PRNumber), nil
}

type PostgresResolver struct {
	db database.Database
}

func (p *PostgresResolver) Resolve(ctx context.Context, signal *WebhookSignal) (string, error) {
	switch signal.Type {
	case "push":
		return p.db.GetBranchWorkflowId(ctx, signal.Owner, signal.Repo, signal.PRBranch)
	default:
		return p.db.GetPrWorkflowId(ctx, signal.Owner, signal.Repo, signal.PRNumber)
	}
}
