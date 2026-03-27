package builder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type LabelPosition struct {
	Page int    `json:"page"`
	X    string `json:"x"`
	Y    string `json:"y"`
}

type LabelSize struct {
	Width  string `json:"width"`
	Height string `json:"height"`
}

type LabelBBox struct {
	Pos  LabelPosition `json:"pos"`
	Size LabelSize     `json:"size"`
}

type typstQueryResult struct {
	Label string     `json:"label"`
	Value *LabelBBox `json:"value"`
}

func TypstQuery(ctx context.Context, typstBin, rootFile, root, selector string, extraFlags ...string) ([]byte, error) {
	if typstBin == "" {
		var err error
		typstBin, err = exec.LookPath("typst")
		if err != nil {
			return nil, fmt.Errorf("failed to find typst binary: %w", err)
		}
	}

	args := []string{"query", rootFile, selector, "--root", root}
	args = append(args, extraFlags...)

	cmd := exec.CommandContext(ctx, typstBin, args...)
	stdout, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			return nil, fmt.Errorf("typst query failed: %w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("typst query failed: %w", err)
	}

	return stdout, nil
}

func QueryLabelBBox(ctx context.Context, typstBin, rootFile, root, label string) (*LabelBBox, error) {
	if typstBin == "" {
		var err error
		typstBin, err = exec.LookPath("typst")
		if err != nil {
			return nil, fmt.Errorf("failed to find typst binary: %w", err)
		}
	}

	if !strings.HasPrefix(label, "<") {
		label = "<" + label
	}
	if !strings.HasSuffix(label, ">") {
		label += ">"
	}

	output, err := TypstQuery(ctx, typstBin, rootFile, root, label, "--field", "value", "--one")
	if err != nil {
		return nil, err
	}

	var bbox LabelBBox
	if err := json.Unmarshal(output, &bbox); err != nil {
		return nil, fmt.Errorf("decode typst bbox: %w", err)
	}

	return &bbox, nil
}

func QueryAllLabels(ctx context.Context, typstBin, rootFile, root string) ([]string, error) {
	output, err := TypstQuery(ctx, typstBin, rootFile, root, "metadata")
	if err != nil {
		return nil, err
	}

	var results []typstQueryResult
	if err := json.Unmarshal(output, &results); err != nil {
		return nil, fmt.Errorf("decode typst labels: %w", err)
	}

	labels := make([]string, 0, len(results))
	for _, result := range results {
		labels = append(labels, strings.Trim(result.Label, "<>"))
	}

	return labels, nil
}

// PointsPerInch is the number of typographic points in one
// inch (1pt = 1/72in). Used to convert between point-based
// coordinates from Typst and pixel-based image coordinates.
const PointsPerInch = 72

// PtToPixels parses a Typst point string like "185.17pt"
// and converts it to pixels at the given PPI.
func PtToPixels(ptStr string, ppi int) (float64, error) {
	if !strings.HasSuffix(ptStr, "pt") {
		return 0, fmt.Errorf("invalid point value %q", ptStr)
	}

	points, err := strconv.ParseFloat(strings.TrimSuffix(ptStr, "pt"), 64)
	if err != nil {
		return 0, fmt.Errorf("parse point value %q: %w", ptStr, err)
	}

	return points * float64(ppi) / PointsPerInch, nil
}
