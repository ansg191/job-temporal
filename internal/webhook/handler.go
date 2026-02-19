package webhook

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

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
		if errors.Is(err, database.ErrNotFound) {
			slog.Info("skipping event", "type", eventType, "reason", err)
			w.WriteHeader(http.StatusOK)
			return
		}
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
	case *github.PushEvent:
		return h.processPush(e)
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

func (h *Handler) processPush(e *github.PushEvent) (*WebhookSignal, error) {
	if e.GetDeleted() {
		return nil, fmt.Errorf("ignoring deleted ref")
	}

	ref := e.GetRef()
	const headPrefix = "refs/heads/"
	if !strings.HasPrefix(ref, headPrefix) {
		return nil, fmt.Errorf("ignoring non-branch ref: %s", ref)
	}
	branch := strings.TrimPrefix(ref, headPrefix)
	if branch == "" {
		return nil, fmt.Errorf("ignoring empty branch ref")
	}

	author := e.GetSender().GetLogin()
	if author == "" {
		author = e.GetPusher().GetName()
	}
	owner, repo := resolvePushRepoCoordinates(e)
	if owner == "" || repo == "" {
		return nil, fmt.Errorf("missing repository owner/repo in push event")
	}

	return &WebhookSignal{
		Type:        "push",
		Action:      "pushed",
		Owner:       owner,
		Repo:        repo,
		PRNumber:    0,
		PRTitle:     "",
		PRBranch:    branch,
		Body:        "",
		AuthorLogin: author,
		ReviewState: "",
		Timestamp:   e.GetRepo().GetPushedAt().Time,
		FilePath:    "",
		Line:        0,
		StartLine:   0,
		DiffHunk:    "",
	}, nil
}

func resolvePushRepoCoordinates(e *github.PushEvent) (string, string) {
	repo := e.GetRepo().GetName()
	owner := e.GetRepo().GetOwner().GetLogin()
	if owner != "" && repo != "" {
		return owner, repo
	}

	fullName := strings.TrimSpace(e.GetRepo().GetFullName())
	if fullName != "" {
		parts := strings.SplitN(fullName, "/", 2)
		if len(parts) == 2 {
			fullOwner := strings.TrimSpace(parts[0])
			fullRepo := strings.TrimSpace(parts[1])
			if owner == "" {
				owner = fullOwner
			}
			if repo == "" {
				repo = fullRepo
			}
		}
	}
	return owner, repo
}

func (h *Handler) sendSignal(ctx context.Context, signal *WebhookSignal) error {
	workflowID, err := h.resolver.Resolve(ctx, signal)
	if err != nil {
		return fmt.Errorf("failed to resolve workflow ID: %w", err)
	}

	// Use empty run ID to signal the latest run
	return h.temporalClient.SignalWorkflow(ctx, workflowID, "", signalNameForSignal(signal), signal)
}

func signalNameForSignal(signal *WebhookSignal) string {
	if signal != nil && signal.Type == "push" {
		return RebuildSignal
	}
	return ReviewAgentSignal
}
