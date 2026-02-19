package jobsource

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileStrategyMatch(t *testing.T) {
	t.Parallel()

	s := NewFileStrategy()

	fileURL, _ := url.Parse("file:///tmp/job.txt")
	if !s.Match(fileURL) {
		t.Fatalf("expected file URL to match")
	}

	httpURL, _ := url.Parse("https://example.com/job")
	if s.Match(httpURL) {
		t.Fatalf("expected non-file URL to not match")
	}
}

func TestFileStrategyFetch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "job.txt")
	content := " Senior platform engineer\nBuild systems\n"
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	rawURL := "file://" + filepath.ToSlash(filePath)
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}

	s := &FileStrategy{baseDir: dir}
	got, err := s.Fetch(context.Background(), u)
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}

	want := strings.TrimSpace(content)
	if got != want {
		t.Fatalf("Fetch = %q, want %q", got, want)
	}
}

func TestFileStrategyFetchOutsideBaseDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outsideFile := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	rawURL := "file://" + filepath.ToSlash(outsideFile)
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}

	s := &FileStrategy{baseDir: dir}
	_, err = s.Fetch(context.Background(), u)
	if err == nil {
		t.Fatalf("expected error for path outside base dir")
	}
}

func TestFileStrategyFetchUnsupportedHost(t *testing.T) {
	t.Parallel()

	u, err := url.Parse("file://remote-host/tmp/job.txt")
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}

	_, err = NewFileStrategy().Fetch(context.Background(), u)
	if err == nil {
		t.Fatalf("expected error for unsupported host")
	}
}
