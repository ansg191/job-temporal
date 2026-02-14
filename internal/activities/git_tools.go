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

type CreatePullRequestRequest struct {
	github.ClientOptions
	Title       string
	Description string
	Head        string
	Base        string
}

func CreatePullRequest(ctx context.Context, req CreatePullRequestRequest) (int, error) {
	client, err := github.NewClient(req.ClientOptions)
	if err != nil {
		return 0, temporal.NewNonRetryableApplicationError(
			"failed to create github client",
			"GithubClientError",
			err,
		)
	}

	return client.CreatePullRequest(ctx, req.Title, req.Description, req.Head, req.Base)
}

type GetBranchHeadSHARequest struct {
	github.ClientOptions
	Branch string
}

func GetBranchHeadSHA(ctx context.Context, req GetBranchHeadSHARequest) (string, error) {
	client, err := github.NewClient(req.ClientOptions)
	if err != nil {
		return "", temporal.NewNonRetryableApplicationError(
			"failed to create github client",
			"GithubClientError",
			err,
		)
	}

	return client.GetBranchHeadSHA(ctx, req.Branch)
}

type GetPullRequestBodyRequest struct {
	github.ClientOptions
	PRNumber int
}

func GetPullRequestBody(ctx context.Context, req GetPullRequestBodyRequest) (string, error) {
	client, err := github.NewClient(req.ClientOptions)
	if err != nil {
		return "", temporal.NewNonRetryableApplicationError(
			"failed to create github client",
			"GithubClientError",
			err,
		)
	}

	return client.GetPullRequestBody(ctx, req.PRNumber)
}

type UpdatePullRequestBodyRequest struct {
	github.ClientOptions
	PRNumber int
	Body     string
}

func UpdatePullRequestBody(ctx context.Context, req UpdatePullRequestBodyRequest) error {
	client, err := github.NewClient(req.ClientOptions)
	if err != nil {
		return temporal.NewNonRetryableApplicationError(
			"failed to create github client",
			"GithubClientError",
			err,
		)
	}

	return client.UpdatePullRequestBody(ctx, req.PRNumber, req.Body)
}

type ProtectBranchRequest struct {
	github.ClientOptions
	Branch string
}

func ProtectBranch(ctx context.Context, req ProtectBranchRequest) error {
	client, err := github.NewClient(req.ClientOptions)
	if err != nil {
		return temporal.NewNonRetryableApplicationError(
			"failed to create github client",
			"GithubClientError",
			err,
		)
	}

	return client.ProtectBranch(ctx, req.Branch)
}
