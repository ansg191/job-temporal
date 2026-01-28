package activities

import (
	"context"
	"path"
	"strings"

	"github.com/ansg191/job-temporal/internal/builder"
	"github.com/ansg191/job-temporal/internal/git"
	"github.com/ansg191/job-temporal/internal/github"
)

type BuildRequest struct {
	github.ClientOptions
	Branch  string `json:"branch"`
	Builder string `json:"builder"`
	File    string `json:"file"`
}

func Build(ctx context.Context, req BuildRequest) (string, error) {
	client, err := github.NewClient(req.ClientOptions)
	if err != nil {
		return "", err
	}

	// Construct a remote URL with GitHub App authentication
	repoRemote, err := client.GetAuthenticatedRemoteURL(ctx)
	if err != nil {
		return "", err
	}

	// Clone the repository
	repo, err := git.NewGitRepo(ctx, repoRemote)
	if err != nil {
		return "", err
	}
	defer repo.Close()

	// Checkout the branch
	if err = repo.SetBranch(ctx, req.Branch); err != nil {
		return "", err
	}

	// Setup the builder
	file := path.Join(repo.Path(), req.File)
	b, err := builder.NewBuilder(req.Builder, builder.WithTypstRootFile(file))
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
