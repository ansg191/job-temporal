package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"

	gh "github.com/google/go-github/v81/github"
)

func newTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	ghClient := gh.NewClient(server.Client())
	baseURL, err := url.Parse(server.URL + "/")
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	ghClient.BaseURL = baseURL
	return &Client{
		Client: ghClient,
		owner:  "acme",
		repo:   "jobs",
	}
}

func TestCreatePullRequestCreatesMissingPurposeLabelsAndAppliesSelectedLabel(t *testing.T) {
	t.Parallel()

	var createdLabels []string
	var appliedLabels []string

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/jobs/pulls":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"number": 12}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/jobs/labels":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"name":"resume"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/jobs/labels":
			var req struct {
				Name string `json:"name"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode create label request: %v", err)
			}
			createdLabels = append(createdLabels, req.Name)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"` + req.Name + `"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/jobs/issues/12/labels":
			if err := json.NewDecoder(r.Body).Decode(&appliedLabels); err != nil {
				t.Fatalf("decode add labels request: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))

	pr, err := client.CreatePullRequest(context.Background(), "title", "body", "head", "base", "resume")
	if err != nil {
		t.Fatalf("CreatePullRequest returned error: %v", err)
	}
	if pr != 12 {
		t.Fatalf("expected PR number 12, got %d", pr)
	}
	if !slices.Equal(createdLabels, []string{"cover letter"}) {
		t.Fatalf("expected missing label creation [cover letter], got %v", createdLabels)
	}
	if !slices.Equal(appliedLabels, []string{"resume"}) {
		t.Fatalf("expected applied labels [resume], got %v", appliedLabels)
	}
}

func TestCreatePullRequestSkipsLabelCreationWhenPurposeLabelsAlreadyExist(t *testing.T) {
	t.Parallel()

	var createLabelCalls int
	var appliedLabels []string

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/jobs/pulls":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"number": 44}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/jobs/labels":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"name":"resume"},{"name":"cover letter"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/jobs/labels":
			createLabelCalls++
			t.Fatalf("did not expect create label call")
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/jobs/issues/44/labels":
			if err := json.NewDecoder(r.Body).Decode(&appliedLabels); err != nil {
				t.Fatalf("decode add labels request: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))

	_, err := client.CreatePullRequest(context.Background(), "title", "body", "head", "base", "cover letter")
	if err != nil {
		t.Fatalf("CreatePullRequest returned error: %v", err)
	}
	if createLabelCalls != 0 {
		t.Fatalf("expected 0 create-label calls, got %d", createLabelCalls)
	}
	if !slices.Equal(appliedLabels, []string{"cover letter"}) {
		t.Fatalf("expected applied labels [cover letter], got %v", appliedLabels)
	}
}

func TestCreatePullRequestReturnsPRNumberWhenEnsureLabelsFails(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/jobs/pulls":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"number": 55}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/jobs/labels":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"internal server error"}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))

	pr, err := client.CreatePullRequest(context.Background(), "title", "body", "head", "base", "resume")
	if err != nil {
		t.Fatalf("expected no error when label ensure fails, got: %v", err)
	}
	if pr != 55 {
		t.Fatalf("expected PR number 55, got %d", pr)
	}
}

func TestCreatePullRequestReturnsPRNumberWhenAddLabelsFails(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/jobs/pulls":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"number": 66}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/jobs/labels":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"name":"resume"},{"name":"cover letter"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/jobs/issues/66/labels":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"internal server error"}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))

	pr, err := client.CreatePullRequest(context.Background(), "title", "body", "head", "base", "resume")
	if err != nil {
		t.Fatalf("expected no error when add labels fails, got: %v", err)
	}
	if pr != 66 {
		t.Fatalf("expected PR number 66, got %d", pr)
	}
}

func TestCreatePullRequestRejectsInvalidPurposeLabel(t *testing.T) {
	t.Parallel()

	var pullsCalls int

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/repos/acme/jobs/pulls" {
			pullsCalls++
			t.Fatalf("should not create PR for invalid purpose label")
		}
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))

	_, err := client.CreatePullRequest(context.Background(), "title", "body", "head", "base", "not-valid")
	if err == nil {
		t.Fatal("expected invalid purpose label error, got nil")
	}
	if pullsCalls != 0 {
		t.Fatalf("expected 0 PR create calls, got %d", pullsCalls)
	}
}
