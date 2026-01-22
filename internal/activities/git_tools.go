package activities

import (
	"context"
	"strings"

	"go.temporal.io/sdk/temporal"

	"github.com/ansg191/job-temporal/internal/git"
)

type ListBranchesRequest struct {
	RepoRemote string `json:"repoRemote"`
}

func ListBranches(ctx context.Context, req ListBranchesRequest) (string, error) {
	// Clone the repository
	repo, err := git.NewGitRepo(ctx, req.RepoRemote)
	if err != nil {
		return "", err
	}
	defer repo.Close()

	// List branches
	branches, err := repo.ListBranches(ctx)
	if err != nil {
		return "", err
	}

	return strings.Join(branches, ", "), nil
}

func CreateBranch(ctx context.Context, remote string, branch string) error {
	repo, err := git.NewGitRepo(ctx, remote)
	if err != nil {
		return err
	}
	defer repo.Close()

	// Check if branch already exists
	branches, err := repo.ListBranches(ctx)
	if err != nil {
		return err
	}
	for _, b := range branches {
		if b == branch {
			return temporal.NewNonRetryableApplicationError("branch already exists", "BranchExistsError", nil)
		}
	}

	return repo.SetBranch(ctx, branch)
}
