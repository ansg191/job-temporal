package activities

import (
	"context"
	"slices"

	"github.com/ansg191/job-temporal/internal/git"
)

type ReadFileRequest struct {
	Path       string   `json:"path"`
	AllowList  []string `json:"allowList"`
	RepoRemote string   `json:"repoRemote"`
	Branch     string   `json:"branch"`
}

func ReadFile(ctx context.Context, req ReadFileRequest) (string, error) {
	// Check that file is in the allowlist
	if !slices.Contains(req.AllowList, req.Path) {
		return "File not allowed to be read", nil
	}

	// Clone the repository
	repo, err := git.NewGitRepo(ctx, req.RepoRemote)
	if err != nil {
		return "", err
	}
	defer repo.Close()

	// Checkout the branch
	if err = repo.SetBranch(ctx, req.Branch); err != nil {
		return "", err
	}

	// Read the file
	content, err := repo.GetFile(ctx, req.Path)
	if err != nil {
		return "", err
	}

	return content, nil
}

type EditFileRequest struct {
	ReadFileRequest
	Patch   string
	Message string
}

func EditFile(ctx context.Context, req EditFileRequest) (string, error) {
	// Check that file is in the allowlist
	if !slices.Contains(req.AllowList, req.Path) {
		return "File not allowed to be edited", nil
	}

	// Clone the repository
	repo, err := git.NewGitRepo(ctx, req.RepoRemote)
	if err != nil {
		return "", err
	}
	defer repo.Close()

	// Checkout the branch
	if err = repo.SetBranch(ctx, req.Branch); err != nil {
		return "", err
	}

	// Get HEAD commit
	head, err := repo.GetHeadCommit(ctx)
	if err != nil {
		return "", err
	}

	// Edit the file
	_, err = repo.EditFile(ctx, head, req.Patch, req.Message)
	if err != nil {
		return err.Error(), nil
	}

	return "Success", nil
}
