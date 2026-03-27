package activities

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"
	"go.temporal.io/sdk/temporal"

	"github.com/ansg191/job-temporal/internal/builder"
	"github.com/ansg191/job-temporal/internal/git"
	"github.com/ansg191/job-temporal/internal/github"
)

const (
	// Render resolution for oracle section crops.
	// 192 PPI gives ~1248px width for full-width sections,
	// enough for GPT-5.4 vision to read 11pt body text.
	oracleDefaultPPI = 192

	// Maximum number of pages to render for a single
	// section, even if the section spans more.
	oracleMaxPageWindow = 3
)

// OracleRenderRequest specifies which section of which
// resume to render and crop for the oracle.
type OracleRenderRequest struct {
	github.ClientOptions
	Branch  string `json:"branch"`
	Builder string `json:"builder"`
	File    string `json:"file"`
	Label   string `json:"label"`
}

// OracleRenderedPage is a single cropped page image that
// has been uploaded to R2 for the oracle to examine.
type OracleRenderedPage struct {
	Page int    `json:"page"`
	URL  string `json:"url"`
}

// RenderOraclePages renders a resume, crops the pages to
// the bounding box of the given Typst label, and uploads
// the cropped PNGs to R2.
//
// Pipeline:
//  1. Clone repo and checkout branch
//  2. Run `typst query` to get the label's bounding box
//  3. Compute per-page crop rectangles from the bbox
//  4. Render the relevant pages as full-page PNGs
//  5. Crop each PNG to the section's bounds
//  6. Upload cropped images to R2
//
// Returns one OracleRenderedPage per cropped image, each
// with a public URL the oracle workflow can pass to the
// vision model.
func RenderOraclePages(ctx context.Context, req OracleRenderRequest) ([]OracleRenderedPage, error) {
	if req.Builder == "" {
		req.Builder = "typst"
	}

	if strings.TrimSpace(req.File) == "" {
		return nil, temporal.NewNonRetryableApplicationError(
			"invalid render arguments",
			"InvalidOracleRenderArguments",
			fmt.Errorf("file is required"),
		)
	}
	if strings.TrimSpace(req.Label) == "" {
		return nil, temporal.NewNonRetryableApplicationError(
			"invalid render arguments",
			"InvalidOracleRenderArguments",
			fmt.Errorf("label is required"),
		)
	}

	// --- Step 1: Clone repo and checkout branch ---

	client, err := github.NewClient(req.ClientOptions)
	if err != nil {
		return nil, err
	}

	repoRemote, err := client.GetAuthenticatedRemoteURL(ctx)
	if err != nil {
		return nil, err
	}

	repo, err := git.NewGitRepo(ctx, repoRemote)
	if err != nil {
		return nil, err
	}
	defer repo.Close()

	if err = repo.SetBranch(ctx, req.Branch); err != nil {
		return nil, err
	}

	rootFile := path.Join(repo.Path(), req.File)

	// --- Step 2: Query label bounding box ---
	// `typst query` returns the label's position (page, x,
	// y) and measured size (width, height) in points.

	bbox, err := builder.QueryLabelBBox(ctx, "", rootFile, repo.Path(), req.Label)
	if err != nil {
		return nil, temporal.NewNonRetryableApplicationError(
			"invalid render arguments",
			"InvalidOracleRenderArguments",
			err,
		)
	}
	if bbox == nil {
		return nil, temporal.NewNonRetryableApplicationError(
			"invalid render arguments",
			"InvalidOracleRenderArguments",
			fmt.Errorf("label bounding box is nil"),
		)
	}

	// --- Step 3: Compute per-page crop rectangles ---
	// Converts the pt-based bbox into pixel-space rects,
	// one per page the section occupies (capped at
	// oracleMaxPageWindow).

	cropRects, err := CropRectsForLabel(*bbox, oracleDefaultPPI, oracleMaxPageWindow)
	if err != nil {
		return nil, temporal.NewNonRetryableApplicationError(
			"invalid render arguments",
			"InvalidOracleRenderArguments",
			err,
		)
	}
	if len(cropRects) == 0 {
		return nil, temporal.NewNonRetryableApplicationError(
			"invalid render arguments",
			"InvalidOracleRenderArguments",
			fmt.Errorf("no crop rectangles generated for label"),
		)
	}

	// Build a page-to-rect lookup and find the page range we
	// need to render. Crop rects are already sorted by page
	// but we scan for min/max to be safe.
	pageStart, pageEnd := cropRects[0].Page, cropRects[0].Page
	rectByPage := make(map[int]PageCropRect, len(cropRects))
	for _, cropRect := range cropRects {
		rectByPage[cropRect.Page] = cropRect
		if cropRect.Page < pageStart {
			pageStart = cropRect.Page
		}
		if cropRect.Page > pageEnd {
			pageEnd = cropRect.Page
		}
	}

	// --- Step 4: Render full-page PNGs ---
	// Typst renders pages N through M as separate PNG files
	// named page-{N}.png, page-{N+1}.png, etc.

	tmpDir, err := os.MkdirTemp(os.TempDir(), "oracle-render-*.d")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	outputPattern := filepath.Join(tmpDir, "page-{0p}.png")
	pageSelector := fmt.Sprintf("%d-%d", pageStart, pageEnd)

	b, err := builder.NewBuilder(
		req.Builder,
		builder.WithTypstRootFile(rootFile),
		builder.WithTypstFormat("png"),
		builder.WithTypstPages(pageSelector),
		builder.WithTypstPPI(oracleDefaultPPI),
		builder.WithPageLimit(0),
	)
	if err != nil {
		return nil, err
	}

	buildResult, err := b.Build(ctx, repo.Path(), outputPattern)
	if err != nil {
		return nil, err
	}
	if !buildResult.Success {
		return nil, temporal.NewNonRetryableApplicationError(
			"build failed",
			ErrTypeBuildFailed,
			nil,
			buildResult.Errors,
		)
	}

	imagePaths, err := filepath.Glob(filepath.Join(tmpDir, "page-*.png"))
	if err != nil {
		return nil, err
	}
	sort.Strings(imagePaths)

	if len(imagePaths) == 0 {
		return nil, fmt.Errorf("oracle render produced no rendered pages")
	}

	// --- Steps 5 & 6: Crop each page and upload to R2 ---
	// Each rendered PNG is a full page. We crop it to the
	// section's bounding box (from step 3), then upload the
	// cropped image. The page number is derived from the
	// file index offset from pageStart.

	r2cfg, err := loadR2Config()
	if err != nil {
		return nil, err
	}

	uploadedPages := make([]OracleRenderedPage, 0, len(imagePaths))
	for idx, imagePath := range imagePaths {
		page := pageStart + idx
		cropRect, ok := rectByPage[page]
		if !ok {
			return nil, fmt.Errorf("missing crop rectangle for page %d", page)
		}

		content, err := os.ReadFile(imagePath)
		if err != nil {
			return nil, err
		}

		cropped, err := CropPNG(content, cropRect.Rect)
		if err != nil {
			return nil, fmt.Errorf("crop page %d: %w", page, err)
		}

		key := "oracle/" + uuid.NewString() + ".png"
		if err = uploadBytesToR2WithContentType(ctx, r2cfg, key, cropped, "image/png"); err != nil {
			return nil, err
		}

		uploadedPages = append(uploadedPages, OracleRenderedPage{
			Page: page,
			URL:  strings.TrimRight(r2cfg.PublicBaseURL, "/") + "/" + key,
		})
	}

	return uploadedPages, nil
}
