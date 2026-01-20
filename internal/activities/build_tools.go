package activities

import (
	"context"
	"strings"

	"github.com/ansg191/job-temporal/internal/builder"
	"github.com/ansg191/job-temporal/internal/git"
)

type BuildRequest struct {
	RepoRemote string `json:"repoRemote"`
	Branch     string `json:"branch"`
	Builder    string `json:"builder"`
}

func Build(ctx context.Context, req BuildRequest) (string, error) {
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

	// Setup the builder
	b, err := builder.NewBuilder(req.Builder, builder.WithTypstRootFile(repo.Path()+"/resume.typ"))
	if err != nil {
		return "", err
	}

	// Perform the build
	result, err := b.Build(ctx, repo.Path())
	if err != nil {
		return "", err
	}

	if result.Success {
		return "Success", nil
	}

	return "Builder returned errors:\n" + strings.Join(result.Errors, "\n"), nil
}
