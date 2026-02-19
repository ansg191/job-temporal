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

// TestFileStrategyFetchPathTraversal verifies that the path-traversal
// protections in Fetch block all common attack vectors.
func TestFileStrategyFetchPathTraversal(t *testing.T) {
	t.Parallel()

	// safeDir is the only directory that should be accessible.
	safeDir := t.TempDir()
	// outsideDir is a sibling directory that must not be reachable.
	outsideDir := t.TempDir()

	// siblingDir has the same string prefix as safeDir (e.g. /tmp/TestXxx/001-evil
	// when safeDir is /tmp/TestXxx/001) to exercise the trailing-separator guard.
	siblingDir := safeDir + "-evil"
	if err := os.MkdirAll(siblingDir, 0o755); err != nil {
		t.Fatalf("create sibling dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(siblingDir) })

	for _, dir := range []string{outsideDir, siblingDir} {
		if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("secret"), 0o644); err != nil {
			t.Fatalf("write secret file: %v", err)
		}
	}

	// symlinkPath is inside safeDir but points to a file in outsideDir.
	symlinkPath := filepath.Join(safeDir, "link.txt")
	if err := os.Symlink(filepath.Join(outsideDir, "secret.txt"), symlinkPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	s := &FileStrategy{baseDir: safeDir}

	tests := []struct {
		name string
		// rawURL is built as a string so we can include raw %-escapes.
		rawURL string
	}{
		{
			name:   "absolute path to /etc/passwd",
			rawURL: "file:///etc/passwd",
		},
		{
			name:   "dot-dot traversal to sibling dir",
			rawURL: "file://" + filepath.ToSlash(filepath.Join(safeDir, "..", filepath.Base(outsideDir), "secret.txt")),
		},
		{
			name: "URL-encoded dot-dot traversal",
			rawURL: "file://" + filepath.ToSlash(safeDir) + "/%2e%2e/" +
				filepath.Base(outsideDir) + "/secret.txt",
		},
		{
			name:   "same-prefix sibling directory",
			rawURL: "file://" + filepath.ToSlash(filepath.Join(siblingDir, "secret.txt")),
		},
		{
			name:   "symlink inside base dir pointing outside",
			rawURL: "file://" + filepath.ToSlash(symlinkPath),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			u, err := url.Parse(tc.rawURL)
			if err != nil {
				t.Fatalf("parse URL %q: %v", tc.rawURL, err)
			}

			_, err = s.Fetch(context.Background(), u)
			if err == nil {
				t.Fatalf("Fetch(%q) succeeded; expected an error blocking path traversal", tc.rawURL)
			}
		})
	}
}

func TestFileStrategyFetchEmptyBaseDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "job.txt")
	if err := os.WriteFile(filePath, []byte("content"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	u, err := url.Parse("file://" + filepath.ToSlash(filePath))
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}

	s := &FileStrategy{} // zero value: baseDir is empty
	_, err = s.Fetch(context.Background(), u)
	if err == nil {
		t.Fatalf("expected error for empty baseDir")
	}
}
