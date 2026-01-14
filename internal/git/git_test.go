package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func ensureGitAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not found in PATH")
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// createBareRemote creates a bare git repo with an initial commit to act as a remote.
// Returns the path to the bare repo.
func createBareRemote(t *testing.T) string {
	t.Helper()

	// Create a bare repo to act as remote
	bareDir, err := os.MkdirTemp(os.TempDir(), "*-bare-repo")
	if err != nil {
		t.Fatalf("failed to create bare repo dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(bareDir) })

	cmd := exec.Command("git", "init", "--bare", bareDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare failed: %v\n%s", err, out)
	}

	// Create a temporary working dir to make the initial commit
	workDir, err := os.MkdirTemp(os.TempDir(), "*-init-repo")
	if err != nil {
		t.Fatalf("failed to create init repo dir: %v", err)
	}
	defer os.RemoveAll(workDir)

	cmd = exec.Command("git", "init", "-b", "main", workDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}

	runGit(t, workDir, "config", "user.email", "test@test.com")
	runGit(t, workDir, "config", "user.name", "Test User")
	runGit(t, workDir, "remote", "add", "origin", bareDir)

	// Create initial commit
	initFile := filepath.Join(workDir, "README.md")
	if err := os.WriteFile(initFile, []byte("# Test Repo\n"), 0o644); err != nil {
		t.Fatalf("write init file failed: %v", err)
	}
	runGit(t, workDir, "add", "README.md")
	runGit(t, workDir, "commit", "-m", "initial commit")
	runGit(t, workDir, "push", "-u", "origin", "main")

	return bareDir
}

func newRepoFixture(t *testing.T) (*Repository, string, string) {
	t.Helper()
	ensureGitAvailable(t)

	ctx := t.Context()

	// Create a local bare repo as remote
	remoteURL := createBareRemote(t)

	repo, err := NewGitRepo(ctx, remoteURL)
	if err != nil {
		t.Fatalf("NewGitRepo failed: %v", err)
	}

	// Create unique branch and commit so tests have deterministic state.
	branchName := fmt.Sprintf("test/%d", time.Now().UnixNano())
	if err := repo.SetBranch(ctx, branchName); err != nil {
		t.Fatalf("SetBranch unique failed: %v", err)
	}

	// Create a unique file per test branch.
	fileName := fmt.Sprintf("TEST_%d.txt", time.Now().UnixNano())
	filePath := filepath.Join(repo.path, fileName)
	content := fmt.Sprintf("test content %s", branchName)
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write test file failed: %v", err)
	}

	runGit(t, repo.path, "add", fileName)
	runGit(t, repo.path, "commit", "-m", "test commit")

	t.Cleanup(func() { _ = repo.Close() })
	return repo, fileName, content
}

func TestNewGitRepoCloneAndClose(t *testing.T) {
	t.Parallel()
	repo, _, _ := newRepoFixture(t)

	if _, err := os.Stat(filepath.Join(repo.path, ".git")); err != nil {
		t.Fatalf("expected cloned repo to contain .git directory: %v", err)
	}

	if err := repo.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if _, err := os.Stat(repo.path); !os.IsNotExist(err) {
		t.Fatalf("expected repo path to be removed after Close, got err=%v", err)
	}
}

func TestRepositoryGetBranch(t *testing.T) {
	t.Parallel()
	repo, _, _ := newRepoFixture(t)
	branch, err := repo.GetBranch(t.Context())
	if err != nil {
		t.Fatalf("GetBranch failed: %v", err)
	}
	if branch == "" {
		t.Fatalf("expected non-empty branch name")
	}
}

func TestRepositorySetBranch(t *testing.T) {
	t.Parallel()
	repo, _, _ := newRepoFixture(t)
	ctx := t.Context()

	if err := repo.SetBranch(ctx, "feature/test-branch"); err != nil {
		t.Fatalf("SetBranch create failed: %v", err)
	}

	branch, err := repo.GetBranch(ctx)
	if err != nil {
		t.Fatalf("GetBranch after create failed: %v", err)
	}
	if branch != "feature/test-branch" {
		t.Fatalf("expected branch 'feature/test-branch', got %q", branch)
	}
}

func TestRepositoryGetHeadCommit(t *testing.T) {
	t.Parallel()
	repo, _, _ := newRepoFixture(t)
	sha, err := repo.getHeadCommit(t.Context())
	if err != nil {
		t.Fatalf("getHeadCommit failed: %v", err)
	}
	if sha == "" {
		t.Fatalf("expected non-empty commit hash")
	}
}

func TestRepositoryGetFile(t *testing.T) {
	t.Parallel()
	repo, fileName, content := newRepoFixture(t)
	data, err := repo.GetFile(t.Context(), fileName)
	if err != nil {
		t.Fatalf("GetFile failed: %v", err)
	}
	if data != content {
		t.Fatalf("expected file content %q, got %q", content, data)
	}
}

// newLocalRepoFixture creates a local bare repo as a "remote" and a cloned working repo.
// This allows testing push/pull operations without needing network access.
// Unlike newRepoFixture, this creates the Repository directly (not via NewGitRepo) to
// preserve access to the remote field for simulating remote changes.
func newLocalRepoFixture(t *testing.T) (*Repository, string, string) {
	t.Helper()
	ensureGitAvailable(t)

	bareDir := createBareRemote(t)

	// Clone from the bare repo
	workDir, err := os.MkdirTemp(os.TempDir(), "*-work-repo")
	if err != nil {
		t.Fatalf("failed to create work repo dir: %v", err)
	}

	cmd := exec.Command("git", "clone", bareDir, workDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git clone failed: %v\n%s", err, out)
	}

	// Configure git user for commits
	runGit(t, workDir, "config", "user.email", "test@test.com")
	runGit(t, workDir, "config", "user.name", "Test User")

	// Create a test file
	fileName := fmt.Sprintf("TEST_%d.txt", time.Now().UnixNano())
	filePath := filepath.Join(workDir, fileName)
	content := "initial content\n"
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write test file failed: %v", err)
	}

	runGit(t, workDir, "add", fileName)
	runGit(t, workDir, "commit", "-m", "initial commit")
	runGit(t, workDir, "push", "origin", "main")

	repo := &Repository{
		path:   workDir,
		remote: bareDir,
	}
	t.Cleanup(func() { _ = repo.Close() })

	return repo, fileName, content
}

func TestRepositoryPullBranch(t *testing.T) {
	t.Parallel()
	repo, _, _ := newLocalRepoFixture(t)
	ctx := t.Context()

	// pullBranch should succeed even when there are no new changes
	if err := repo.pullBranch(ctx); err != nil {
		t.Fatalf("pullBranch failed: %v", err)
	}
}

func TestRepositoryPullBranchWithChanges(t *testing.T) {
	t.Parallel()
	repo, fileName, _ := newLocalRepoFixture(t)
	ctx := t.Context()

	// Simulate a change in the "remote" by creating another clone,
	// making a commit, and pushing
	cloneDir, err := os.MkdirTemp(os.TempDir(), "*-clone-repo")
	if err != nil {
		t.Fatalf("failed to create clone dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(cloneDir) })

	cmd := exec.Command("git", "clone", repo.remote, cloneDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git clone failed: %v\n%s", err, out)
	}

	runGit(t, cloneDir, "config", "user.email", "test@test.com")
	runGit(t, cloneDir, "config", "user.name", "Test User")

	// Make a change and push
	cloneFilePath := filepath.Join(cloneDir, fileName)
	if err := os.WriteFile(cloneFilePath, []byte("updated from clone"), 0o644); err != nil {
		t.Fatalf("write file in clone failed: %v", err)
	}
	runGit(t, cloneDir, "add", fileName)
	runGit(t, cloneDir, "commit", "-m", "update from clone")
	runGit(t, cloneDir, "push", "origin", "main")

	// Now pull in the original repo
	if err := repo.pullBranch(ctx); err != nil {
		t.Fatalf("pullBranch failed: %v", err)
	}

	// Verify the file was updated
	data, err := repo.GetFile(ctx, fileName)
	if err != nil {
		t.Fatalf("GetFile failed: %v", err)
	}
	if data != "updated from clone" {
		t.Fatalf("expected updated content, got %q", data)
	}
}

func TestRepositoryEditFileValidation(t *testing.T) {
	t.Parallel()
	repo, _, _ := newLocalRepoFixture(t)
	ctx := t.Context()

	tests := []struct {
		name     string
		commitID string
		patch    string
		msg      string
		wantErr  string
	}{
		{
			name:     "empty commit ID",
			commitID: "",
			patch:    "some patch",
			msg:      "some message",
			wantErr:  "commit ID is required",
		},
		{
			name:     "empty patch",
			commitID: "abc123",
			patch:    "",
			msg:      "some message",
			wantErr:  "patch is required",
		},
		{
			name:     "empty message",
			commitID: "abc123",
			patch:    "some patch",
			msg:      "",
			wantErr:  "commit message is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := repo.EditFile(ctx, tt.commitID, tt.patch, tt.msg)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestRepositoryEditFile(t *testing.T) {
	t.Parallel()
	repo, fileName, _ := newLocalRepoFixture(t)
	ctx := t.Context()

	// Get current HEAD
	headCommit, err := repo.getHeadCommit(ctx)
	if err != nil {
		t.Fatalf("getHeadCommit failed: %v", err)
	}

	// Create a patch that modifies the file
	patch := fmt.Sprintf(`--- a/%s
+++ b/%s
@@ -1 +1 @@
-initial content
+edited content
`, fileName, fileName)

	newHead, err := repo.EditFile(ctx, headCommit, patch, "test edit commit")
	if err != nil {
		t.Fatalf("EditFile failed: %v", err)
	}

	if newHead == "" {
		t.Fatal("expected non-empty new HEAD commit")
	}
	if newHead == headCommit {
		t.Fatal("expected new HEAD to differ from old HEAD")
	}

	// Verify the file was edited
	data, err := repo.GetFile(ctx, fileName)
	if err != nil {
		t.Fatalf("GetFile failed: %v", err)
	}
	if data != "edited content\n" {
		t.Fatalf("expected 'edited content\\n', got %q", data)
	}
}

func TestRepositoryEditFileAncestorCheck(t *testing.T) {
	t.Parallel()
	repo, fileName, _ := newLocalRepoFixture(t)
	ctx := t.Context()

	// Get current HEAD
	oldHead, err := repo.getHeadCommit(ctx)
	if err != nil {
		t.Fatalf("getHeadCommit failed: %v", err)
	}

	// Make a new commit to move HEAD forward
	filePath := filepath.Join(repo.path, fileName)
	if err := os.WriteFile(filePath, []byte("intermediate content\n"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
	runGit(t, repo.path, "add", fileName)
	runGit(t, repo.path, "commit", "-m", "intermediate commit")
	runGit(t, repo.path, "push", "origin", "main")

	// Now try to edit with old HEAD - should work since old HEAD is ancestor
	patch := fmt.Sprintf(`--- a/%s
+++ b/%s
@@ -1 +1 @@
-intermediate content
+final content
`, fileName, fileName)

	newHead, err := repo.EditFile(ctx, oldHead, patch, "edit after ancestor check")
	if err != nil {
		t.Fatalf("EditFile failed: %v", err)
	}

	if newHead == "" {
		t.Fatal("expected non-empty new HEAD commit")
	}

	// Verify the file was edited
	data, err := repo.GetFile(ctx, fileName)
	if err != nil {
		t.Fatalf("GetFile failed: %v", err)
	}
	if data != "final content\n" {
		t.Fatalf("expected 'final content\\n', got %q", data)
	}
}
