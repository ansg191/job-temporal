package github

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"slices"

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
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
}

var supportedPRPurposeLabels = []string{"resume", "cover letter"}

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

func (c *Client) CreatePullRequest(ctx context.Context, title, description, head, base, purposeLabel string) (int, error) {
	if !slices.Contains(supportedPRPurposeLabels, purposeLabel) {
		return 0, fmt.Errorf("invalid purpose label %q", purposeLabel)
	}

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

	if err := c.ensurePurposeLabelsExist(ctx); err != nil {
		log.Printf("warning: failed to ensure purpose labels exist for PR #%d: %v", pr.GetNumber(), err)
		return pr.GetNumber(), nil
	}
	if _, _, err := c.Issues.AddLabelsToIssue(ctx, c.owner, c.repo, pr.GetNumber(), []string{purposeLabel}); err != nil {
		log.Printf("warning: failed to add purpose label %q to PR #%d: %v", purposeLabel, pr.GetNumber(), err)
		return pr.GetNumber(), nil
	}

	return pr.GetNumber(), nil
}

func (c *Client) ensurePurposeLabelsExist(ctx context.Context) error {
	existing := make(map[string]struct{}, len(supportedPRPurposeLabels))
	opts := &github.ListOptions{PerPage: 100}
	for {
		labels, resp, err := c.Issues.ListLabels(ctx, c.owner, c.repo, opts)
		if err != nil {
			return err
		}
		for _, label := range labels {
			existing[label.GetName()] = struct{}{}
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	for _, label := range supportedPRPurposeLabels {
		if _, ok := existing[label]; ok {
			continue
		}
		name := label
		_, _, err := c.Issues.CreateLabel(ctx, c.owner, c.repo, &github.Label{Name: &name})
		if err != nil {
			if ghErr, ok := err.(*github.ErrorResponse); ok && ghErr.Response != nil && ghErr.Response.StatusCode == http.StatusUnprocessableEntity {
				// Label already created by a concurrent request.
				continue
			}
			return err
		}
	}
	return nil
}

func (c *Client) GetBranchHeadSHA(ctx context.Context, branch string) (string, error) {
	ref, _, err := c.Git.GetRef(ctx, c.owner, c.repo, "heads/"+branch)
	if err != nil {
		return "", err
	}
	return ref.GetObject().GetSHA(), nil
}

func (c *Client) GetPullRequestBody(ctx context.Context, prNumber int) (string, error) {
	pr, _, err := c.PullRequests.Get(ctx, c.owner, c.repo, prNumber)
	if err != nil {
		return "", err
	}
	return pr.GetBody(), nil
}

func (c *Client) UpdatePullRequestBody(ctx context.Context, prNumber int, body string) error {
	_, _, err := c.PullRequests.Edit(
		ctx,
		c.owner,
		c.repo,
		prNumber,
		&github.PullRequest{
			Body: &body,
		},
	)
	return err
}

func (c *Client) ProtectBranch(ctx context.Context, branch string) error {
	lockBranch := true
	allowForcePushes := false
	allowDeletions := false

	_, _, err := c.Repositories.UpdateBranchProtection(
		ctx,
		c.owner,
		c.repo,
		branch,
		&github.ProtectionRequest{
			RequiredStatusChecks:       nil,
			RequiredPullRequestReviews: nil,
			EnforceAdmins:              false,
			Restrictions:               nil,
			AllowForcePushes:           &allowForcePushes,
			AllowDeletions:             &allowDeletions,
			LockBranch:                 &lockBranch,
		},
	)
	return err
}
