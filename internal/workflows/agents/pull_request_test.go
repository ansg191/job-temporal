package agents

import "testing"

func TestPurposeLabelForBuildTargetResume(t *testing.T) {
	t.Parallel()

	label, err := purposeLabelForBuildTarget(BuildTargetResume)
	if err != nil {
		t.Fatalf("expected no error for resume target, got %v", err)
	}
	if label != "resume" {
		t.Fatalf("expected resume label, got %q", label)
	}
}

func TestPurposeLabelForBuildTargetCoverLetter(t *testing.T) {
	t.Parallel()

	label, err := purposeLabelForBuildTarget(BuildTargetCoverLetter)
	if err != nil {
		t.Fatalf("expected no error for cover letter target, got %v", err)
	}
	if label != "cover letter" {
		t.Fatalf("expected cover letter label, got %q", label)
	}
}

func TestPurposeLabelForBuildTargetInvalidReturnsError(t *testing.T) {
	t.Parallel()

	label, err := purposeLabelForBuildTarget(BuildTarget(99))
	if err == nil {
		t.Fatal("expected error for invalid build target, got nil")
	}
	if label != "" {
		t.Fatalf("expected empty label on error, got %q", label)
	}
}
