package activities

import (
	"context"
	"encoding/json"
	"path"

	"github.com/ansg191/job-temporal/internal/builder"
	"github.com/ansg191/job-temporal/internal/git"
	"github.com/ansg191/job-temporal/internal/github"
)

type ListLabelsRequest struct {
	github.ClientOptions
	Branch string `json:"branch"`
	File   string `json:"file"`
}

func ListLabels(ctx context.Context, req ListLabelsRequest) (string, error) {
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

	if err := repo.SetBranch(ctx, req.Branch); err != nil {
		return "", err
	}

	rootFile := path.Join(repo.Path(), req.File)

	labels, err := builder.QueryAllLabels(ctx, "", rootFile, repo.Path())
	if err != nil {
		return "", err
	}

	result, err := json.Marshal(labels)
	if err != nil {
		return "", err
	}

	return string(result), nil
}
