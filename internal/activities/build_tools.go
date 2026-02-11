package activities

import (
	"context"
	"os"
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
	tmpFile, err := os.CreateTemp(os.TempDir(), "build-*.pdf")
	if err != nil {
		return "", err
	}
	if err = tmpFile.Close(); err != nil {
		return "", err
	}
	defer os.Remove(tmpFile.Name())

	result, err := runBuild(ctx, req.ClientOptions, req.Branch, req.Builder, req.File, tmpFile.Name())
	if err != nil {
		return "", err
	}

	if result.Success {
		return "Success", nil
	}

	return "Builder returned errors:\n" + strings.Join(result.Errors, "\n"), nil
}

func runBuild(
	ctx context.Context,
	clientOpts github.ClientOptions,
	branch string,
	builderName string,
	file string,
	outputPath string,
) (*builder.BuildResult, error) {
	client, err := github.NewClient(clientOpts)
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

	if err = repo.SetBranch(ctx, branch); err != nil {
		return nil, err
	}

	rootFile := path.Join(repo.Path(), file)
	pageLimit := pageLimitForBuildFile(file)
	b, err := builder.NewBuilder(
		builderName,
		builder.WithTypstRootFile(rootFile),
		builder.WithPageLimit(pageLimit),
	)
	if err != nil {
		return nil, err
	}

	return b.Build(ctx, repo.Path(), outputPath)
}

func pageLimitForBuildFile(file string) int {
	switch file {
	case "resume.typ":
		// Allow resume generation/review passes to overshoot by one page.
		return 2
	default:
		return 1
	}
}
