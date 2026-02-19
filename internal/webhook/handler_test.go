package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-github/v81/github"

	"github.com/ansg191/job-temporal/internal/database"
)

type staticResolver struct {
	workflowID string
	err        error
}

func (s *staticResolver) Resolve(_ context.Context, _ *WebhookSignal) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.workflowID, nil
}

func TestProcessPushExtractsBranch(t *testing.T) {
	t.Parallel()

	h := &Handler{}
	e := &github.PushEvent{
		Ref:     github.Ptr("refs/heads/feature/rebuild"),
		Deleted: github.Ptr(false),
		Repo: &github.PushEventRepository{
			Name:  github.Ptr("resume"),
			Owner: &github.User{Login: github.Ptr("ansg191")},
		},
		Sender: &github.User{Login: github.Ptr("alice")},
	}

	signal, err := h.processPush(e)
	if err != nil {
		t.Fatalf("processPush returned error: %v", err)
	}
	if signal.Type != "push" {
		t.Fatalf("expected push type, got %q", signal.Type)
	}
	if signal.PRBranch != "feature/rebuild" {
		t.Fatalf("expected branch feature/rebuild, got %q", signal.PRBranch)
	}
	if signal.Owner != "ansg191" || signal.Repo != "resume" {
		t.Fatalf("unexpected repo coordinates: %s/%s", signal.Owner, signal.Repo)
	}
	if signal.AuthorLogin != "alice" {
		t.Fatalf("expected sender author alice, got %q", signal.AuthorLogin)
	}
}

func TestProcessPushFallsBackToFullNameWhenOwnerLoginMissing(t *testing.T) {
	t.Parallel()

	h := &Handler{}
	e := &github.PushEvent{
		Ref:     github.Ptr("refs/heads/feature/rebuild"),
		Deleted: github.Ptr(false),
		Repo: &github.PushEventRepository{
			Name:     github.Ptr(""),
			FullName: github.Ptr("ansg191/resume"),
			Owner:    &github.User{},
		},
		Sender: &github.User{Login: github.Ptr("alice")},
	}

	signal, err := h.processPush(e)
	if err != nil {
		t.Fatalf("processPush returned error: %v", err)
	}
	if signal.Owner != "ansg191" || signal.Repo != "resume" {
		t.Fatalf("expected fallback owner/repo ansg191/resume, got %s/%s", signal.Owner, signal.Repo)
	}
}

func TestProcessPushIgnoresNonBranchRef(t *testing.T) {
	t.Parallel()

	h := &Handler{}
	e := &github.PushEvent{
		Ref:     github.Ptr("refs/tags/v1.0.0"),
		Deleted: github.Ptr(false),
	}

	_, err := h.processPush(e)
	if err == nil {
		t.Fatal("expected error for non-branch ref, got nil")
	}
}

func TestProcessPushIgnoresDeletedRef(t *testing.T) {
	t.Parallel()

	h := &Handler{}
	e := &github.PushEvent{
		Ref:     github.Ptr("refs/heads/feature/rebuild"),
		Deleted: github.Ptr(true),
	}

	_, err := h.processPush(e)
	if err == nil {
		t.Fatal("expected error for deleted ref, got nil")
	}
}

func TestSignalNameForSignal(t *testing.T) {
	t.Parallel()

	if got := signalNameForSignal(&WebhookSignal{Type: "push"}); got != RebuildSignal {
		t.Fatalf("expected rebuild signal, got %q", got)
	}
	if got := signalNameForSignal(&WebhookSignal{Type: "issue_comment"}); got != ReviewAgentSignal {
		t.Fatalf("expected review signal, got %q", got)
	}
}

func TestServeHTTPNotFoundWorkflowIsSkipped(t *testing.T) {
	t.Parallel()

	secret := "webhook-secret"
	payload := []byte(`{"ref":"refs/heads/feature/rebuild","deleted":false,"repository":{"name":"resume","owner":{"login":"ansg191"}},"sender":{"login":"alice"}}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", signWebhookPayload(secret, payload))

	rr := httptest.NewRecorder()
	h := NewHandler(nil, nil, secret).WithResolver(&staticResolver{err: database.ErrNotFound})
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
}

func signWebhookPayload(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
