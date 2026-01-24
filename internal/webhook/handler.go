package webhook

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/google/go-github/v81/github"
	"go.temporal.io/sdk/client"

	"github.com/ansg191/job-temporal/internal/database"
)

// Handler handles GitHub webhook events and sends Temporal signals.
type Handler struct {
	temporalClient client.Client
	webhookSecret  []byte
	resolver       WorkflowIDResolver
}

// NewHandler creates a new webhook handler.
func NewHandler(tc client.Client, db database.Database, webhookSecret string) *Handler {
	return &Handler{
		temporalClient: tc,
		webhookSecret:  []byte(webhookSecret),
		resolver: &PostgresResolver{
			db: db,
		},
	}
}

// WithResolver sets a custom workflow ID resolver.
func (h *Handler) WithResolver(resolver WorkflowIDResolver) *Handler {
	h.resolver = resolver
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	payload, err := h.validateAndReadPayload(r)
	if err != nil {
		slog.Error("failed to validate payload", "error", err)
		http.Error(w, "invalid payload", http.StatusUnauthorized)
		return
	}

	eventType := github.WebHookType(r)
	event, err := github.ParseWebHook(eventType, payload)
	if err != nil {
		slog.Error("failed to parse webhook", "error", err)
		http.Error(w, "failed to parse webhook", http.StatusBadRequest)
		return
	}

	signal, err := h.processEvent(eventType, event)
	if err != nil {
		slog.Info("skipping event", "type", eventType, "reason", err)
		w.WriteHeader(http.StatusOK)
		return
	}

	if err := h.sendSignal(r.Context(), signal); err != nil {
		slog.Error("failed to send signal", "error", err)
		http.Error(w, "failed to send signal", http.StatusInternalServerError)
		return
	}

	slog.Info("signal sent",
		"type", signal.Type,
		"owner", signal.Owner,
		"repo", signal.Repo,
		"pr", signal.PRNumber,
	)
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) validateAndReadPayload(r *http.Request) ([]byte, error) {
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()

	return github.ValidatePayloadFromBody(r.Header.Get("Content-Type"), bytes.NewReader(payload), r.Header.Get("X-Hub-Signature-256"), h.webhookSecret)
}

func (h *Handler) processEvent(eventType string, event any) (*WebhookSignal, error) {
	switch e := event.(type) {
	case *github.PullRequestReviewEvent:
		return h.processPullRequestReview(e)
	case *github.IssueCommentEvent:
		return h.processIssueComment(e)
	case *github.PullRequestReviewCommentEvent:
		return h.processPullRequestReviewComment(e)
	case *github.PullRequestEvent:
		return h.processPullRequest(e)
	default:
		return nil, fmt.Errorf("unsupported event type: %s", eventType)
	}
}

func (h *Handler) processPullRequestReview(e *github.PullRequestReviewEvent) (*WebhookSignal, error) {
	if e.GetAction() != "submitted" {
		return nil, fmt.Errorf("ignoring action: %s", e.GetAction())
	}

	return &WebhookSignal{
		Type:        "pull_request_review",
		Action:      e.GetAction(),
		Owner:       e.GetRepo().GetOwner().GetLogin(),
		Repo:        e.GetRepo().GetName(),
		PRNumber:    e.GetPullRequest().GetNumber(),
		PRTitle:     e.GetPullRequest().GetTitle(),
		PRBranch:    e.GetPullRequest().GetHead().GetRef(),
		Body:        e.GetReview().GetBody(),
		AuthorLogin: e.GetReview().GetUser().GetLogin(),
		ReviewState: e.GetReview().GetState(),
		Timestamp:   e.GetReview().GetSubmittedAt().Time,
	}, nil
}

func (h *Handler) processIssueComment(e *github.IssueCommentEvent) (*WebhookSignal, error) {
	if e.GetAction() != "created" {
		return nil, fmt.Errorf("ignoring action: %s", e.GetAction())
	}

	// Only process comments on pull requests
	if e.GetIssue().GetPullRequestLinks() == nil {
		return nil, fmt.Errorf("ignoring comment on non-PR issue")
	}

	return &WebhookSignal{
		Type:        "issue_comment",
		Action:      e.GetAction(),
		Owner:       e.GetRepo().GetOwner().GetLogin(),
		Repo:        e.GetRepo().GetName(),
		PRNumber:    e.GetIssue().GetNumber(),
		PRTitle:     e.GetIssue().GetTitle(),
		PRBranch:    "", // Not available in issue comment events
		Body:        e.GetComment().GetBody(),
		AuthorLogin: e.GetComment().GetUser().GetLogin(),
		ReviewState: "",
		Timestamp:   e.GetComment().GetCreatedAt().Time,
	}, nil
}

func (h *Handler) processPullRequestReviewComment(e *github.PullRequestReviewCommentEvent) (*WebhookSignal, error) {
	if e.GetAction() != "created" {
		return nil, fmt.Errorf("ignoring action: %s", e.GetAction())
	}

	return &WebhookSignal{
		Type:        "pull_request_review_comment",
		Action:      e.GetAction(),
		Owner:       e.GetRepo().GetOwner().GetLogin(),
		Repo:        e.GetRepo().GetName(),
		PRNumber:    e.GetPullRequest().GetNumber(),
		PRTitle:     e.GetPullRequest().GetTitle(),
		PRBranch:    e.GetPullRequest().GetHead().GetRef(),
		Body:        e.GetComment().GetBody(),
		AuthorLogin: e.GetComment().GetUser().GetLogin(),
		ReviewState: "",
		Timestamp:   e.GetComment().GetCreatedAt().Time,
		FilePath:    e.GetComment().GetPath(),
		Line:        e.GetComment().GetLine(),
		StartLine:   e.GetComment().GetStartLine(),
		DiffHunk:    e.GetComment().GetDiffHunk(),
	}, nil
}

func (h *Handler) processPullRequest(e *github.PullRequestEvent) (*WebhookSignal, error) {
	if e.GetAction() != "closed" {
		return nil, fmt.Errorf("ignoring action: %s", e.GetAction())
	}

	return &WebhookSignal{
		Type:        "pull_request",
		Action:      e.GetAction(),
		Owner:       e.GetRepo().GetOwner().GetLogin(),
		Repo:        e.GetRepo().GetName(),
		PRNumber:    e.GetPullRequest().GetNumber(),
		PRTitle:     e.GetPullRequest().GetTitle(),
		PRBranch:    e.GetPullRequest().GetHead().GetRef(),
		Body:        e.GetPullRequest().GetBody(),
		AuthorLogin: e.GetPullRequest().GetUser().GetLogin(),
		ReviewState: "",
		Timestamp:   e.GetPullRequest().GetClosedAt().Time,
		FilePath:    "",
		Line:        0,
		StartLine:   0,
		DiffHunk:    "",
	}, nil
}

func (h *Handler) sendSignal(ctx context.Context, signal *WebhookSignal) error {
	workflowID, err := h.resolver.Resolve(ctx, signal.Owner, signal.Repo, signal.PRNumber)
	if err != nil {
		return fmt.Errorf("failed to resolve workflow ID: %w", err)
	}

	// Use empty run ID to signal the latest run
	return h.temporalClient.SignalWorkflow(ctx, workflowID, "", ReviewAgentSignal, signal)
}
