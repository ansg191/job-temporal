package builder

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func makeTempPDFPath(t *testing.T) string {
	t.Helper()

	f, err := os.CreateTemp(t.TempDir(), "out-*.pdf")
	if err != nil {
		t.Fatalf("CreateTemp() unexpected error: %v", err)
	}
	if err = f.Close(); err != nil {
		t.Fatalf("Close() unexpected error: %v", err)
	}
	return f.Name()
}

func TestNewTypstBuilder_WithTypstPath(t *testing.T) {
	t.Parallel()

	customPath := "/custom/path/to/typst"
	builder, err := newTypstBuilder(WithTypstPath(customPath))

	if err != nil {
		t.Fatalf("newTypstBuilder() unexpected error: %v", err)
	}

	if builder.execPath != customPath {
		t.Errorf("execPath = %q, want %q", builder.execPath, customPath)
	}
}

func TestNewTypstBuilder_WithTypstRootFile(t *testing.T) {
	t.Parallel()

	rootFile := "resume.typ"
	// Need to provide a valid typst path or have typst installed
	builder, err := newTypstBuilder(
		WithTypstPath("/fake/typst"),
		WithTypstRootFile(rootFile),
	)

	if err != nil {
		t.Fatalf("newTypstBuilder() unexpected error: %v", err)
	}

	if builder.rootFile != rootFile {
		t.Errorf("rootFile = %q, want %q", builder.rootFile, rootFile)
	}
}

func TestNewTypstBuilder_WithMultipleOptions(t *testing.T) {
	t.Parallel()

	customPath := "/custom/path/to/typst"
	rootFile := "main.typ"

	builder, err := newTypstBuilder(
		WithTypstPath(customPath),
		WithTypstRootFile(rootFile),
	)

	if err != nil {
		t.Fatalf("newTypstBuilder() unexpected error: %v", err)
	}

	if builder.execPath != customPath {
		t.Errorf("execPath = %q, want %q", builder.execPath, customPath)
	}

	if builder.rootFile != rootFile {
		t.Errorf("rootFile = %q, want %q", builder.rootFile, rootFile)
	}
}

func TestNewTypstBuilder_WithPageLimit(t *testing.T) {
	t.Parallel()

	builder, err := newTypstBuilder(
		WithTypstPath("/fake/typst"),
		WithPageLimit(5),
	)

	if err != nil {
		t.Fatalf("newTypstBuilder() unexpected error: %v", err)
	}

	if builder.pageLimit != 5 {
		t.Errorf("pageLimit = %d, want 5", builder.pageLimit)
	}
}

func TestNewTypstBuilder_DefaultPageLimit(t *testing.T) {
	t.Parallel()

	builder, err := newTypstBuilder(WithTypstPath("/fake/typst"))

	if err != nil {
		t.Fatalf("newTypstBuilder() unexpected error: %v", err)
	}

	if builder.pageLimit != 1 {
		t.Errorf("pageLimit = %d, want 1 (default)", builder.pageLimit)
	}
}

func TestNewTypstBuilder_DefaultExecPath(t *testing.T) {
	t.Parallel()

	// Skip if typst is not installed
	expectedPath, err := exec.LookPath("typst")
	if err != nil {
		t.Skip("typst not installed, skipping default path test")
	}

	builder, err := newTypstBuilder()
	if err != nil {
		t.Fatalf("newTypstBuilder() unexpected error: %v", err)
	}

	if builder.execPath != expectedPath {
		t.Errorf("execPath = %q, want %q", builder.execPath, expectedPath)
	}
}

func TestNewTypstBuilder_TypstNotFound(t *testing.T) {
	t.Parallel()

	// Check if typst is installed - if it is, we can't test the "not found" case
	_, err := exec.LookPath("typst")
	if err == nil {
		t.Skip("typst is installed, cannot test 'not found' case")
	}

	_, err = newTypstBuilder()
	if err == nil {
		t.Error("newTypstBuilder() expected error when typst not found, got nil")
	}
}

func TestParseTypstErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		output   string
		expected []string
	}{
		{
			name:     "empty output",
			output:   "",
			expected: nil,
		},
		{
			name:     "single error",
			output:   "file.typ:26:5: error: unclosed delimiter",
			expected: []string{"file.typ:26:5: error: unclosed delimiter"},
		},
		{
			name:   "multiple errors",
			output: "file.typ:26:5: error: unclosed delimiter\nfile.typ:33:0: error: unexpected equals sign",
			expected: []string{
				"file.typ:26:5: error: unclosed delimiter",
				"file.typ:33:0: error: unexpected equals sign",
			},
		},
		{
			name:     "mixed output with non-error lines",
			output:   "some info\nfile.typ:26:5: error: unclosed delimiter\nmore info",
			expected: []string{"file.typ:26:5: error: unclosed delimiter"},
		},
		{
			name:     "input file not found error",
			output:   "error: input file not found (searched at nonexistent.typ)",
			expected: []string{"error: input file not found (searched at nonexistent.typ)"},
		},
		{
			name:   "mixed compilation and file errors",
			output: "error: input file not found (searched at foo.typ)\nfile.typ:10:1: error: unknown variable",
			expected: []string{
				"error: input file not found (searched at foo.typ)",
				"file.typ:10:1: error: unknown variable",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := parseTypstErrors(tt.output)

			if len(result) != len(tt.expected) {
				t.Errorf("parseTypstErrors() returned %d errors, want %d", len(result), len(tt.expected))
				return
			}

			for i, err := range result {
				if err != tt.expected[i] {
					t.Errorf("parseTypstErrors()[%d] = %q, want %q", i, err, tt.expected[i])
				}
			}
		})
	}
}

// Build method tests

func TestTypstBuilder_Build_Success(t *testing.T) {
	t.Parallel()

	_, err := exec.LookPath("typst")
	if err != nil {
		t.Skip("typst not installed")
	}

	builder, err := newTypstBuilder(WithTypstRootFile("fixtures/valid.typ"))
	if err != nil {
		t.Fatalf("newTypstBuilder() unexpected error: %v", err)
	}

	ctx := context.Background()
	result, err := builder.Build(ctx, ".", makeTempPDFPath(t))

	if err != nil {
		t.Errorf("Build() unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("Build() Success = false, want true")
	}
	if len(result.Errors) != 0 {
		t.Errorf("Build() Errors = %v, want empty", result.Errors)
	}
}

func TestTypstBuilder_Build_CompilationError(t *testing.T) {
	t.Parallel()

	_, err := exec.LookPath("typst")
	if err != nil {
		t.Skip("typst not installed")
	}

	builder, err := newTypstBuilder(WithTypstRootFile("fixtures/invalid.typ"))
	if err != nil {
		t.Fatalf("newTypstBuilder() unexpected error: %v", err)
	}

	ctx := context.Background()
	result, err := builder.Build(ctx, ".", makeTempPDFPath(t))

	if err != nil {
		t.Errorf("Build() unexpected error: %v", err)
	}
	if result.Success {
		t.Errorf("Build() Success = true, want false")
	}
	if len(result.Errors) == 0 {
		t.Errorf("Build() Errors is empty, want errors")
	}
	// Check error format
	for _, e := range result.Errors {
		if !strings.Contains(e, ": error:") {
			t.Errorf("Error %q doesn't match expected format", e)
		}
	}
}

func TestTypstBuilder_Build_PageLimitExceeded(t *testing.T) {
	t.Parallel()

	_, err := exec.LookPath("typst")
	if err != nil {
		t.Skip("typst not installed")
	}

	builder, err := newTypstBuilder(WithTypstRootFile("fixtures/multipage.typ"))
	if err != nil {
		t.Fatalf("newTypstBuilder() unexpected error: %v", err)
	}

	ctx := context.Background()
	result, err := builder.Build(ctx, ".", makeTempPDFPath(t))

	if err != nil {
		t.Fatalf("Build() unexpected error: %v", err)
	}
	if result.Success {
		t.Errorf("Build() Success = true for multi-page resume, want false")
	}
	if len(result.Errors) == 0 {
		t.Errorf("Build() Errors is empty, want page limit error")
	}
	// Check error message contains expected text
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "page limit exceeded") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Build() Errors %v doesn't contain 'page limit exceeded'", result.Errors)
	}
}

func TestTypstBuilder_Build_WithCustomPageLimit(t *testing.T) {
	t.Parallel()

	_, err := exec.LookPath("typst")
	if err != nil {
		t.Skip("typst not installed")
	}

	// 2-page document should pass with page limit of 2
	builder, err := newTypstBuilder(
		WithTypstRootFile("fixtures/multipage.typ"),
		WithPageLimit(2),
	)
	if err != nil {
		t.Fatalf("newTypstBuilder() unexpected error: %v", err)
	}

	ctx := context.Background()
	result, err := builder.Build(ctx, ".", makeTempPDFPath(t))

	if err != nil {
		t.Fatalf("Build() unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("Build() Success = false with page limit 2, want true. Errors: %v", result.Errors)
	}
}

func TestTypstBuilder_Build_NoPageLimit(t *testing.T) {
	t.Parallel()

	_, err := exec.LookPath("typst")
	if err != nil {
		t.Skip("typst not installed")
	}

	// Any document should pass with page limit disabled (0)
	builder, err := newTypstBuilder(
		WithTypstRootFile("fixtures/multipage.typ"),
		WithPageLimit(0),
	)
	if err != nil {
		t.Fatalf("newTypstBuilder() unexpected error: %v", err)
	}

	ctx := context.Background()
	result, err := builder.Build(ctx, ".", makeTempPDFPath(t))

	if err != nil {
		t.Fatalf("Build() unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("Build() Success = false with page limit disabled, want true. Errors: %v", result.Errors)
	}
}

func TestTypstBuilder_Build_MissingRootFile(t *testing.T) {
	t.Parallel()

	_, err := exec.LookPath("typst")
	if err != nil {
		t.Skip("typst not installed")
	}

	builder, err := newTypstBuilder(
		WithTypstRootFile("fixtures/nonexistent.typ"),
	)
	if err != nil {
		t.Fatalf("newTypstBuilder() unexpected error: %v", err)
	}

	ctx := context.Background()
	result, err := builder.Build(ctx, ".", makeTempPDFPath(t))

	if err != nil {
		t.Fatalf("Build() unexpected error: %v", err)
	}
	if result.Success {
		t.Errorf("Build() Success = true for missing root file, want false")
	}
	if len(result.Errors) == 0 {
		t.Errorf("Build() Errors is empty, want errors about missing file")
	}
	// Check error contains expected text
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "input file not found") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Build() Errors %v doesn't contain 'input file not found'", result.Errors)
	}
}

func TestTypstBuilder_Build_Cancelled(t *testing.T) {
	t.Parallel()

	_, err := exec.LookPath("typst")
	if err != nil {
		t.Skip("typst not installed")
	}

	builder, err := newTypstBuilder(
		WithTypstRootFile("fixtures/valid.typ"),
	)
	if err != nil {
		t.Fatalf("newTypstBuilder() unexpected error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	result, err := builder.Build(ctx, ".", makeTempPDFPath(t))

	// With a cancelled context, the command should fail
	// Either err is non-nil or result.Success is false
	if err == nil && result.Success {
		t.Errorf("Build() expected failure for cancelled context, got success")
	}
}
