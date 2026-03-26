package activities

import (
	"context"
	"fmt"

	"github.com/ansg191/job-temporal/internal/git"
	"github.com/ansg191/job-temporal/internal/github"
)

const letterReviewResultFormatKey = "letter_review_result"

var LetterReviewTextFormat = GenerateTextFormat[ReviewLetterContentOutput](letterReviewResultFormatKey)

type ReviewLetterContentRequest struct {
	github.ClientOptions
	Branch string `json:"branch"`
	Job    string `json:"job"`
}

type ReviewLetterContentIssue struct {
	IssueType string `json:"issue_type"`
	Severity  string `json:"severity"`
	Location  string `json:"location"`
	Evidence  string `json:"evidence"`
	FixHint   string `json:"fix_hint"`
}

func (i ReviewLetterContentIssue) GetSeverity() string { return i.Severity }

type ReviewLetterContentOutput struct {
	Summary string                     `json:"summary"`
	Issues  []ReviewLetterContentIssue `json:"issues"`
}

type ReadLetterContentRequest struct {
	github.ClientOptions
	Branch string `json:"branch"`
}

func ReadLetterContent(ctx context.Context, req ReadLetterContentRequest) (string, error) {
	client, err := github.NewClient(req.ClientOptions)
	if err != nil {
		return "", err
	}

	repoRemote, err := client.GetAuthenticatedRemoteURL(ctx)
	if err != nil {
		return "", err
	}

	repo, err := git.NewGitRepo(ctx, repoRemote)
	if err != nil {
		return "", err
	}
	defer repo.Close()

	if err = repo.SetBranch(ctx, req.Branch); err != nil {
		return "", err
	}

	content, err := repo.GetFile(ctx, "letter.typ")
	if err != nil {
		return "", fmt.Errorf("failed to read letter.typ: %w", err)
	}

	return content, nil
}
