package github

import (
	"context"
	"fmt"
	"net/http"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v81/github"
)

type Client struct {
	*github.Client
	itr   *ghinstallation.Transport
	owner string
	repo  string
}

type ClientOptions struct {
	Owner string
	Repo  string
}

func NewClient(opts ClientOptions) (*Client, error) {
	itr, err := getTransport()
	if err != nil {
		return nil, err
	}

	client := github.NewClient(&http.Client{Transport: itr})
	return &Client{client, itr, opts.Owner, opts.Repo}, nil
}

// GetAuthenticatedRemoteURL returns a git HTTPS URL with embedded authentication token.
// The URL format is: https://x-access-token:<TOKEN>@github.com/<owner>/<repo>.git
func (c *Client) GetAuthenticatedRemoteURL(ctx context.Context) (string, error) {
	token, err := c.itr.Token(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get installation token: %w", err)
	}
	return fmt.Sprintf("https://x-access-token:%s@github.com/%s/%s.git", token, c.owner, c.repo), nil
}

func (c *Client) Owner() string {
	return c.owner
}

func (c *Client) Repo() string {
	return c.repo
}

func (c *Client) ListBranches(ctx context.Context) ([]string, error) {
	var branches []string
	page := 1

	for {
		res, _, err := c.Repositories.ListBranches(ctx, c.owner, c.repo, &github.BranchListOptions{
			ListOptions: github.ListOptions{
				Page:    page,
				PerPage: 100,
			},
		})
		if err != nil {
			return nil, err
		}
		if len(res) == 0 {
			break
		}
		for _, b := range res {
			branches = append(branches, b.GetName())
		}
		page++
	}

	return branches, nil
}

func (c *Client) getDefaultBranch(ctx context.Context) (string, error) {
	repo, _, err := c.Repositories.Get(ctx, c.owner, c.repo)
	if err != nil {
		return "", err
	}
	return repo.GetDefaultBranch(), nil
}

// CreateBranch creates a new branch in the repository from the default branch.
func (c *Client) CreateBranch(ctx context.Context, branch string) error {
	defaultBranch, err := c.getDefaultBranch(ctx)
	if err != nil {
		return err
	}

	// Get the sha of the default branch
	ref, _, err := c.Git.GetRef(ctx, c.owner, c.repo, "heads/"+defaultBranch)
	if err != nil {
		return err
	}

	// Create the new branch from the reference
	_, _, err = c.Git.CreateRef(ctx, c.owner, c.repo, github.CreateRef{
		Ref: "refs/heads/" + branch,
		SHA: ref.GetObject().GetSHA(),
	})
	return err
}

func (c *Client) CreatePullRequest(ctx context.Context, title, description, head, base string) (int, error) {
	pr, _, err := c.PullRequests.Create(
		ctx,
		c.owner,
		c.repo,
		&github.NewPullRequest{
			Title:               &title,
			Head:                &head,
			Base:                &base,
			Body:                &description,
			MaintainerCanModify: github.Ptr(true),
		},
	)
	if err != nil {
		return 0, err
	}
	return pr.GetNumber(), nil
}
