package activities

import (
	"context"
	"strings"

	"go.temporal.io/sdk/temporal"

	"github.com/ansg191/job-temporal/internal/github"
)

type ListBranchesRequest struct {
	github.ClientOptions
}

func ListBranches(ctx context.Context, req ListBranchesRequest) (string, error) {
	client, err := github.NewClient(req.ClientOptions)
	if err != nil {
		return "", temporal.NewNonRetryableApplicationError(
			"failed to create github client",
			"GithubClientError",
			err,
		)
	}

	branches, err := client.ListBranches(ctx)
	if err != nil {
		return "", err
	}
	return strings.Join(branches, ", "), nil
}

type CreateBranchRequest struct {
	github.ClientOptions
	Branch string
}

func CreateBranch(ctx context.Context, req CreateBranchRequest) error {
	client, err := github.NewClient(req.ClientOptions)
	if err != nil {
		return temporal.NewNonRetryableApplicationError(
			"failed to create github client",
			"GithubClientError",
			err,
		)
	}

	return client.CreateBranch(ctx, req.Branch)
}
