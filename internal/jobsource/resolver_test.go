package jobsource

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

type stubStrategy struct {
	name      string
	matchHost string
	result    string
	err       error
}

func (s stubStrategy) Name() string { return s.name }

func (s stubStrategy) Match(u *url.URL) bool {
	return u.Hostname() == s.matchHost
}

func (s stubStrategy) Fetch(_ context.Context, _ *url.URL) (string, error) {
	return s.result, s.err
}

func TestResolverResolveRawText(t *testing.T) {
	t.Parallel()

	r := NewResolver()
	got, err := r.Resolve(context.Background(), " Senior backend engineer in Go ")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got != "Senior backend engineer in Go" {
		t.Fatalf("Resolve = %q, want %q", got, "Senior backend engineer in Go")
	}
}

func TestResolverResolveURL(t *testing.T) {
	t.Parallel()

	r := NewResolver(stubStrategy{
		name:      "example",
		matchHost: "example.com",
		result:    "resolved description",
	})

	got, err := r.Resolve(context.Background(), "https://example.com/job/123")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got != "resolved description" {
		t.Fatalf("Resolve = %q, want %q", got, "resolved description")
	}
}

func TestResolverResolveUnsupportedURL(t *testing.T) {
	t.Parallel()

	r := NewResolver()
	_, err := r.Resolve(context.Background(), "https://example.com/job/123")
	if err == nil {
		t.Fatalf("expected error for unsupported URL")
	}
}

func TestResolverResolveFileURL(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "job.txt")
	if err := os.WriteFile(filePath, []byte("  File URL job description  "), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	r := NewResolver(NewFileStrategy())
	rawURL := "file://" + filepath.ToSlash(filePath)

	got, err := r.Resolve(context.Background(), rawURL)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got != "File URL job description" {
		t.Fatalf("Resolve = %q, want %q", got, "File URL job description")
	}
}
