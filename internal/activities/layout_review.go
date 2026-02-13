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
	layoutReviewDefaultPPI      = 192
	layoutReviewDefaultPage     = 1
	layoutReviewMaxPageWindow   = 3
	layoutReviewResultFormatKey = "layout_review_result"
)

var LayoutReviewTextFormat = GenerateTextFormat[ReviewPDFLayoutOutput](layoutReviewResultFormatKey)

type ReviewPDFLayoutRequest struct {
	github.ClientOptions
	Branch    string `json:"branch"`
	Builder   string `json:"builder"`
	File      string `json:"file"`
	PageStart int    `json:"page_start"`
	PageEnd   int    `json:"page_end"`
	Focus     string `json:"focus"`
}

type ReviewPDFLayoutIssue struct {
	Page      int    `json:"page"`
	IssueType string `json:"issue_type"`
	Severity  string `json:"severity"`
	Evidence  string `json:"evidence"`
	FixHint   string `json:"fix_hint"`
}

type ReviewPDFLayoutOutput struct {
	Summary      string                 `json:"summary"`
	CheckedPages []int                  `json:"checked_pages"`
	Issues       []ReviewPDFLayoutIssue `json:"issues"`
}

type LayoutReviewRenderedPage struct {
	Page int    `json:"page"`
	URL  string `json:"url"`
}

type RenderLayoutReviewPagesRequest struct {
	ReviewPDFLayoutRequest
}

func RenderLayoutReviewPages(ctx context.Context, req RenderLayoutReviewPagesRequest) ([]LayoutReviewRenderedPage, error) {
	if req.Builder == "" {
		req.Builder = "typst"
	}
	pageStart, pageEnd, err := normalizeLayoutPageRange(req.PageStart, req.PageEnd)
	if err != nil {
		return nil, temporal.NewNonRetryableApplicationError(
			"invalid page range",
			"InvalidLayoutReviewArguments",
			err,
		)
	}

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

	tmpDir, err := os.MkdirTemp(os.TempDir(), "layout-review-*.d")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	outputPattern := filepath.Join(tmpDir, "page-{0p}.png")
	pageSelector := fmt.Sprintf("%d-%d", pageStart, pageEnd)
	rootFile := path.Join(repo.Path(), req.File)

	b, err := builder.NewBuilder(
		req.Builder,
		builder.WithTypstRootFile(rootFile),
		builder.WithTypstFormat("png"),
		builder.WithTypstPages(pageSelector),
		builder.WithTypstPPI(layoutReviewDefaultPPI),
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
		return nil, fmt.Errorf("layout review produced no rendered pages")
	}

	r2cfg, err := loadR2Config()
	if err != nil {
		return nil, err
	}

	uploadedPages := make([]LayoutReviewRenderedPage, 0, len(imagePaths))
	for idx, imagePath := range imagePaths {
		content, err := os.ReadFile(imagePath)
		if err != nil {
			return nil, err
		}
		key := "layout-review/" + uuid.NewString() + ".png"
		if err = uploadBytesToR2WithContentType(ctx, r2cfg, key, content, "image/png"); err != nil {
			return nil, err
		}
		uploadedPages = append(uploadedPages, LayoutReviewRenderedPage{
			Page: pageStart + idx,
			URL:  strings.TrimRight(r2cfg.PublicBaseURL, "/") + "/" + key,
		})
	}
	return uploadedPages, nil
}

func normalizeLayoutPageRange(pageStart int, pageEnd int) (int, int, error) {
	if pageStart <= 0 {
		pageStart = layoutReviewDefaultPage
	}
	if pageEnd <= 0 {
		pageEnd = pageStart
	}
	if pageEnd < pageStart {
		return 0, 0, fmt.Errorf("page_end must be greater than or equal to page_start")
	}
	if (pageEnd - pageStart + 1) > layoutReviewMaxPageWindow {
		pageEnd = pageStart + (layoutReviewMaxPageWindow - 1)
	}
	return pageStart, pageEnd, nil
}
