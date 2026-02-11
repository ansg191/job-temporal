package builder

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

type typstBuilder struct {
	execPath  string
	rootFile  string
	pageLimit int // 0 = no limit, default = 1
}

func WithTypstPath(path string) func(Builder) {
	return func(b Builder) {
		b.(*typstBuilder).execPath = path
	}
}

func WithTypstRootFile(path string) func(Builder) {
	return func(b Builder) {
		b.(*typstBuilder).rootFile = path
	}
}

func WithPageLimit(limit int) func(Builder) {
	return func(b Builder) {
		switch b.(type) {
		case *typstBuilder:
			b.(*typstBuilder).pageLimit = limit
		}
	}
}

func newTypstBuilder(opts ...func(Builder)) (*typstBuilder, error) {
	ret := &typstBuilder{pageLimit: 1} // default to 1 page for resumes
	for _, opt := range opts {
		opt(ret)
	}

	if ret.execPath == "" {
		execPath, err := exec.LookPath("typst")
		if err != nil {
			return nil, fmt.Errorf("failed to find typst binary: %w", err)
		}
		ret.execPath = execPath
	}

	return ret, nil
}

func (t *typstBuilder) Build(ctx context.Context, path string, outputPath string) (*BuildResult, error) {
	if outputPath == "" {
		return nil, fmt.Errorf("output path is required")
	}

	// Run command and capture output
	cmd := exec.CommandContext(ctx, t.execPath, "compile", t.rootFile, outputPath, "--root", path, "--diagnostic-format=short")
	slog.InfoContext(ctx, "Running typst command", "cmd", cmd.String())
	output, err := cmd.CombinedOutput()

	// Parse errors from output
	errors := parseTypstErrors(string(output))

	// If command failed, return result with errors
	if err != nil {
		return &BuildResult{
			Success: false,
			Errors:  errors,
		}, nil
	}

	// Check page limit
	if t.pageLimit > 0 {
		pageCount, err := api.PageCountFile(outputPath)
		if err != nil {
			return nil, fmt.Errorf("failed to count PDF pages: %w", err)
		}
		slog.DebugContext(ctx, "PDF page count", "count", pageCount, "limit", t.pageLimit)
		if pageCount > t.pageLimit {
			return &BuildResult{
				Success: false,
				Errors: []string{
					fmt.Sprintf("page limit exceeded: document has %d page(s), maximum allowed is %d",
						pageCount, t.pageLimit),
				},
			}, nil
		}
	}

	// Success case
	return &BuildResult{
		Success: true,
		Errors:  nil,
	}, nil
}

func parseTypstErrors(output string) []string {
	var errors []string
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Match format: file:line:col: error: message
		// or: error: message (e.g., "error: input file not found")
		if strings.Contains(line, ": error:") || strings.HasPrefix(line, "error:") {
			errors = append(errors, line)
		}
	}
	return errors
}
