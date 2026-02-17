package workflows

import (
	"testing"

	"github.com/ansg191/job-temporal/internal/workflows/agents"
)

func TestResolvePurposeEmptyReturnsError(t *testing.T) {
	t.Parallel()

	purpose, target, err := resolvePurpose("")
	if err == nil {
		t.Fatal("expected error for empty purpose, got nil")
	}
	if purpose != "" {
		t.Fatalf("expected empty branch purpose on error, got %q", purpose)
	}
	if target != 0 {
		t.Fatalf("expected zero build target on error, got %d", target)
	}
}

func TestResolvePurposeResumeMapping(t *testing.T) {
	t.Parallel()

	purpose, target, err := resolvePurpose("resume")
	if err != nil {
		t.Fatalf("expected no error for valid purpose, got %v", err)
	}
	if purpose != agents.BranchNameAgentPurposeResume {
		t.Fatalf("expected %q, got %q", agents.BranchNameAgentPurposeResume, purpose)
	}
	if target != agents.BuildTargetResume {
		t.Fatalf("expected %d, got %d", agents.BuildTargetResume, target)
	}
}

func TestResolvePurposeCoverLetterMapping(t *testing.T) {
	t.Parallel()

	purpose, target, err := resolvePurpose("cover_letter")
	if err != nil {
		t.Fatalf("expected no error for valid purpose, got %v", err)
	}
	if purpose != agents.BranchNameAgentPurposeCoverLetter {
		t.Fatalf("expected %q, got %q", agents.BranchNameAgentPurposeCoverLetter, purpose)
	}
	if target != agents.BuildTargetCoverLetter {
		t.Fatalf("expected %d, got %d", agents.BuildTargetCoverLetter, target)
	}
}
