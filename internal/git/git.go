package git

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Repository references a git repository on the system stored in a tmp directory
type Repository struct {
	path   string
	remote string
}

func NewGitRepo(ctx context.Context, remote string) (*Repository, error) {
	tmpPath, err := os.MkdirTemp(os.TempDir(), "*-gitrepo")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}

	ret := &Repository{
		path:   tmpPath,
		remote: remote,
	}
	if err := ret.clone(ctx); err != nil {
		_ = ret.Close()
		return nil, err
	}
	return ret, nil
}

func (r *Repository) Close() error {
	return os.RemoveAll(r.path)
}

// gitCmd creates an exec.Cmd for git with proper environment variables set
func (r *Repository) gitCmd(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", r.path}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
	)
	return cmd
}

// Clones the repo into the path
func (r *Repository) clone(ctx context.Context) error {
	if r.remote == "" {
		return fmt.Errorf("remote repository is required")
	}

	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", r.remote, r.path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errMsg := strings.TrimSpace(stderr.String()); errMsg != "" {
			return fmt.Errorf("git clone failed: %s: %w", errMsg, err)
		}
		return fmt.Errorf("git clone failed: %w", err)
	}

	return nil
}

// GetBranch returns the current branch of the repository.
func (r *Repository) GetBranch(ctx context.Context) (string, error) {
	cmd := r.gitCmd(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errMsg := strings.TrimSpace(stderr.String()); errMsg != "" {
			return "", fmt.Errorf("git rev-parse failed: %s: %w", errMsg, err)
		}
		return "", fmt.Errorf("git rev-parse failed: %w", err)
	}

	branch := strings.TrimSpace(stdout.String())
	if branch == "" {
		return "", fmt.Errorf("git rev-parse returned empty branch name")
	}

	return branch, nil
}

// SetBranch sets the current branch of the repository, creating if necessary
func (r *Repository) SetBranch(ctx context.Context, branch string) error {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return fmt.Errorf("branch name is required")
	}

	verify := r.gitCmd(ctx, "rev-parse", "--verify", fmt.Sprintf("refs/heads/%s", branch))
	var verifyOut, verifyErr bytes.Buffer
	verify.Stdout = &verifyOut
	verify.Stderr = &verifyErr

	branchExists := verify.Run() == nil

	var cmd *exec.Cmd
	if !branchExists {
		cmd = r.gitCmd(ctx, "checkout", "-b", branch)
	} else {
		cmd = r.gitCmd(ctx, "checkout", branch)
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errMsg := strings.TrimSpace(stderr.String()); errMsg != "" {
			return fmt.Errorf("git checkout failed: %s: %w", errMsg, err)
		}
		return fmt.Errorf("git checkout failed: %w", err)
	}

	return nil
}

// getHeadCommit returns the SHA of the HEAD commit of the repository
func (r *Repository) getHeadCommit(ctx context.Context) (string, error) {
	cmd := r.gitCmd(ctx, "rev-parse", "HEAD")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errMsg := strings.TrimSpace(stderr.String()); errMsg != "" {
			return "", fmt.Errorf("git rev-parse HEAD failed: %s: %w", errMsg, err)
		}
		return "", fmt.Errorf("git rev-parse HEAD failed: %w", err)
	}

	sha := strings.TrimSpace(stdout.String())
	if sha == "" {
		return "", fmt.Errorf("git rev-parse HEAD returned empty hash")
	}

	return sha, nil
}

// GetFile returns the contents of a file at the HEAD commit
func (r *Repository) GetFile(ctx context.Context, filePath string) (string, error) {
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return "", fmt.Errorf("file path is required")
	}

	cmd := r.gitCmd(ctx, "show", fmt.Sprintf("HEAD:%s", filePath))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errMsg := strings.TrimSpace(stderr.String()); errMsg != "" {
			return "", fmt.Errorf("git show failed: %s: %w", errMsg, err)
		}
		return "", fmt.Errorf("git show failed: %w", err)
	}

	return stdout.String(), nil
}

// pullBranch updates the current branch to the latest changes from the remote
func (r *Repository) pullBranch(ctx context.Context) error {
	// Fetch latest from remote
	fetchCmd := r.gitCmd(ctx, "fetch", "origin")
	var fetchStderr bytes.Buffer
	fetchCmd.Stderr = &fetchStderr

	if err := fetchCmd.Run(); err != nil {
		if errMsg := strings.TrimSpace(fetchStderr.String()); errMsg != "" {
			return fmt.Errorf("git fetch failed: %s: %w", errMsg, err)
		}
		return fmt.Errorf("git fetch failed: %w", err)
	}

	// Get current branch name
	branch, err := r.GetBranch(ctx)
	if err != nil {
		return fmt.Errorf("failed to get current branch: %w", err)
	}

	// Pull changes from origin for the current branch
	pullCmd := r.gitCmd(ctx, "pull", "origin", branch)
	var pullStderr bytes.Buffer
	pullCmd.Stderr = &pullStderr

	if err := pullCmd.Run(); err != nil {
		if errMsg := strings.TrimSpace(pullStderr.String()); errMsg != "" {
			return fmt.Errorf("git pull failed: %s: %w", errMsg, err)
		}
		return fmt.Errorf("git pull failed: %w", err)
	}

	return nil
}

// EditFile takes a previous commit SHA, a patch diff, a commit message, and edits the file accordingly.
// It follows the following procedure to update the branch safely:
// 1. Pull the latest changes from the remote
// 2. Verifies that the previous commit SHA is the HEAD (if not, rebases if possible, else aborts)
// 3. Applies the patch (aborting if failure)
// 4. Commits with message
// 5. Pushes branch to remote
// 6. Returns the new HEAD commit SHA.
func (r *Repository) EditFile(ctx context.Context, commitID, patch, msg string) (string, error) {
	commitID = strings.TrimSpace(commitID)
	msg = strings.TrimSpace(msg)

	if commitID == "" {
		return "", fmt.Errorf("commit ID is required")
	}
	if strings.TrimSpace(patch) == "" {
		return "", fmt.Errorf("patch is required")
	}
	// Ensure patch ends with newline (required by git apply)
	if !strings.HasSuffix(patch, "\n") {
		patch = patch + "\n"
	}
	if msg == "" {
		return "", fmt.Errorf("commit message is required")
	}

	// Step 1: Pull the latest changes from the remote
	if err := r.pullBranch(ctx); err != nil {
		return "", fmt.Errorf("failed to pull latest changes: %w", err)
	}

	// Step 2: Verify that the previous commit SHA is the HEAD
	headCommit, err := r.getHeadCommit(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get HEAD commit: %w", err)
	}

	if headCommit != commitID {
		// HEAD has moved since the patch was created. Check if we can safely apply
		// the patch by verifying the old commit is an ancestor of the current HEAD.
		// If it is, the patch may still apply cleanly to the new HEAD.
		verifyCmd := r.gitCmd(ctx, "merge-base", "--is-ancestor", commitID, headCommit)
		if err := verifyCmd.Run(); err != nil {
			return "", fmt.Errorf("commit %s is not an ancestor of HEAD %s, cannot safely apply patch", commitID, headCommit)
		}
	}

	// Step 3: Apply the patch
	applyCmd := r.gitCmd(ctx, "apply", "--index")
	applyCmd.Stdin = strings.NewReader(patch)
	var applyStderr bytes.Buffer
	applyCmd.Stderr = &applyStderr

	if err := applyCmd.Run(); err != nil {
		if errMsg := strings.TrimSpace(applyStderr.String()); errMsg != "" {
			return "", fmt.Errorf("git apply failed: %s: %w", errMsg, err)
		}
		return "", fmt.Errorf("git apply failed: %w", err)
	}

	// Step 4: Commit with message
	commitCmd := r.gitCmd(ctx, "commit", "-m", msg)
	var commitStderr bytes.Buffer
	commitCmd.Stderr = &commitStderr

	if err := commitCmd.Run(); err != nil {
		if errMsg := strings.TrimSpace(commitStderr.String()); errMsg != "" {
			return "", fmt.Errorf("git commit failed: %s: %w", errMsg, err)
		}
		return "", fmt.Errorf("git commit failed: %w", err)
	}

	// Step 5: Push branch to remote
	branch, err := r.GetBranch(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get current branch: %w", err)
	}

	pushCmd := r.gitCmd(ctx, "push", "origin", branch)
	var pushStderr bytes.Buffer
	pushCmd.Stderr = &pushStderr

	if err := pushCmd.Run(); err != nil {
		if errMsg := strings.TrimSpace(pushStderr.String()); errMsg != "" {
			return "", fmt.Errorf("git push failed: %s: %w", errMsg, err)
		}
		return "", fmt.Errorf("git push failed: %w", err)
	}

	// Step 6: Return the new HEAD commit SHA
	newHead, err := r.getHeadCommit(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get new HEAD commit: %w", err)
	}

	return newHead, nil
}
