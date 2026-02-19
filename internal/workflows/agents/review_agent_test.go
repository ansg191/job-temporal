package agents

import (
	"strings"
	"testing"
)

func TestRewriteArtifactLinesForSuccessReplacesURLAndClearsError(t *testing.T) {
	t.Parallel()

	body := "Summary\nPDF Artifact: https://old.example/file.pdf\nPDF Artifact Error: previous failure\nDetails"
	updated, oldURL := rewriteArtifactLinesForSuccess(body, "https://new.example/file.pdf")

	if oldURL != "https://old.example/file.pdf" {
		t.Fatalf("expected old URL to be extracted, got %q", oldURL)
	}
	if !strings.Contains(updated, "PDF Artifact: https://new.example/file.pdf") {
		t.Fatalf("expected updated body to contain new artifact URL, got %q", updated)
	}
	if strings.Contains(updated, prArtifactErrorLinePrefix) {
		t.Fatalf("expected stale artifact error line removed, got %q", updated)
	}
}

func TestRewriteArtifactLinesForSuccessAppendsWhenMissing(t *testing.T) {
	t.Parallel()

	updated, oldURL := rewriteArtifactLinesForSuccess("Summary", "https://new.example/file.pdf")
	if oldURL != "" {
		t.Fatalf("expected empty old URL for missing artifact line, got %q", oldURL)
	}
	if !strings.Contains(updated, "PDF Artifact: https://new.example/file.pdf") {
		t.Fatalf("expected artifact line to be appended, got %q", updated)
	}
}

func TestRewriteArtifactLinesForFailureBlanksURLAndAddsError(t *testing.T) {
	t.Parallel()

	body := "Summary\nPDF Artifact: https://old.example/file.pdf\nDetails"
	updated := rewriteArtifactLinesForFailure(body, "build failed\nline 2")

	if !strings.Contains(updated, "PDF Artifact:") {
		t.Fatalf("expected artifact line to exist, got %q", updated)
	}
	if strings.Contains(updated, "PDF Artifact: https://old.example/file.pdf") {
		t.Fatalf("expected old artifact URL to be removed, got %q", updated)
	}
	if !strings.Contains(updated, "PDF Artifact Error: build failed line 2") {
		t.Fatalf("expected sanitized error line, got %q", updated)
	}
}

func TestSanitizeArtifactErrorReasonDefaultsForEmpty(t *testing.T) {
	t.Parallel()

	if got := sanitizeArtifactErrorReason("   "); got != "build failed" {
		t.Fatalf("expected default message, got %q", got)
	}
}
