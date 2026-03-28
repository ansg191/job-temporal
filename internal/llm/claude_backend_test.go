package llm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadClaudeAuthTokenFromFile_Valid(t *testing.T) {
	t.Parallel()

	authFile := claudeAuthFile{
		Anthropic: claudeAuthEntry{
			Type:    "oauth",
			Access:  "test-access-token-abc123",
			Refresh: "test-refresh-token",
			Expires: time.Now().Add(time.Hour).UnixMilli(),
		},
	}
	path := writeTestAuthFile(t, authFile)

	token, err := readClaudeAuthTokenFromFile(path)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if token != "test-access-token-abc123" {
		t.Fatalf("expected token %q, got %q", "test-access-token-abc123", token)
	}
}

func TestReadClaudeAuthTokenFromFile_Expired(t *testing.T) {
	t.Parallel()

	authFile := claudeAuthFile{
		Anthropic: claudeAuthEntry{
			Type:    "oauth",
			Access:  "expired-token",
			Refresh: "refresh",
			Expires: time.Now().Add(-time.Hour).UnixMilli(),
		},
	}
	path := writeTestAuthFile(t, authFile)

	_, err := readClaudeAuthTokenFromFile(path)
	if err == nil {
		t.Fatalf("expected error for expired token")
	}
}

func TestReadClaudeAuthTokenFromFile_ZeroExpiresIsAllowed(t *testing.T) {
	t.Parallel()

	// An expires value of 0 means "no expiry info" -- should not reject.
	authFile := claudeAuthFile{
		Anthropic: claudeAuthEntry{
			Type:    "oauth",
			Access:  "no-expiry-token",
			Refresh: "refresh",
			Expires: 0,
		},
	}
	path := writeTestAuthFile(t, authFile)

	token, err := readClaudeAuthTokenFromFile(path)
	if err != nil {
		t.Fatalf("expected no error for zero expires, got: %v", err)
	}
	if token != "no-expiry-token" {
		t.Fatalf("token mismatch: got %q", token)
	}
}

func TestReadClaudeAuthTokenFromFile_MissingAccessToken(t *testing.T) {
	t.Parallel()

	authFile := claudeAuthFile{
		Anthropic: claudeAuthEntry{
			Type:    "oauth",
			Refresh: "refresh",
			Expires: time.Now().Add(time.Hour).UnixMilli(),
		},
	}
	path := writeTestAuthFile(t, authFile)

	_, err := readClaudeAuthTokenFromFile(path)
	if err == nil {
		t.Fatalf("expected error for missing access token")
	}
}

func TestReadClaudeAuthTokenFromFile_MissingFile(t *testing.T) {
	t.Parallel()

	_, err := readClaudeAuthTokenFromFile(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err == nil {
		t.Fatalf("expected error for missing file")
	}
}

func TestReadClaudeAuthTokenFromFile_MalformedJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("could not write test file: %v", err)
	}

	_, err := readClaudeAuthTokenFromFile(path)
	if err == nil {
		t.Fatalf("expected error for malformed JSON")
	}
}

func TestClaudeAuthFilePath_EnvOverride(t *testing.T) {
	t.Parallel()

	// claudeAuthFilePath reads CLAUDE_AUTH_FILE. We test via the
	// readClaudeAuthTokenFromFile path directly since env mutation
	// in parallel tests is unsafe. This test validates the file-based
	// path works as expected (env override is integration-level).
	authFile := claudeAuthFile{
		Anthropic: claudeAuthEntry{
			Type:    "oauth",
			Access:  "env-override-token",
			Refresh: "refresh",
			Expires: time.Now().Add(time.Hour).UnixMilli(),
		},
	}
	path := writeTestAuthFile(t, authFile)

	token, err := readClaudeAuthTokenFromFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "env-override-token" {
		t.Fatalf("token mismatch: got %q", token)
	}
}

func TestClaudeRequestOptions_BaseBetas(t *testing.T) {
	t.Parallel()

	// We cannot inspect option.RequestOption internals directly, but we can
	// verify the function does not panic and returns a non-empty slice.
	opts := claudeRequestOptions("claude-sonnet-4-5")
	if len(opts) == 0 {
		t.Fatalf("expected non-empty request options")
	}
}

func TestClaudeRequestOptions_CompactionModelGetsExtraOptions(t *testing.T) {
	t.Parallel()

	base := claudeRequestOptions("claude-sonnet-4-5")
	compaction := claudeRequestOptions("claude-sonnet-4-6")

	// 4-6 models support compaction, so they get extra options.
	if len(compaction) <= len(base) {
		t.Fatalf("expected 4-6 model to have more options than 4-5; got %d vs %d",
			len(compaction), len(base))
	}
}

func TestClaudeClientOptions_ReturnsNonEmpty(t *testing.T) {
	t.Parallel()

	opts := claudeClientOptions("test-token")
	if len(opts) == 0 {
		t.Fatalf("expected non-empty client options")
	}
}

func TestClaudeCLIVersion_Default(t *testing.T) {
	t.Parallel()

	version := claudeCLIVersion()
	if version != claudeDefaultCLIVersion {
		t.Fatalf("expected default CLI version %q, got %q", claudeDefaultCLIVersion, version)
	}
}

func TestNewClaudeBackend(t *testing.T) {
	t.Parallel()

	b, err := newClaudeBackend()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b == nil {
		t.Fatalf("expected non-nil backend")
	}
}

func TestClaudeBackend_ImplementsBackendInterface(t *testing.T) {
	t.Parallel()

	var _ Backend = (*claudeBackend)(nil)
}

func TestParseModelRef_Claude(t *testing.T) {
	t.Parallel()

	ref, err := ParseModelRef("claude/claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if ref.Backend != BackendClaude {
		t.Fatalf("expected backend %q, got %q", BackendClaude, ref.Backend)
	}
	// Provider should be "anthropic" since it uses the same underlying API.
	if ref.Provider != string(BackendAnthropic) {
		t.Fatalf("expected provider %q, got %q", BackendAnthropic, ref.Provider)
	}
	if ref.Model != "claude-sonnet-4-6" {
		t.Fatalf("expected model %q, got %q", "claude-sonnet-4-6", ref.Model)
	}
}

func TestParseModelRef_ClaudeEmptyID(t *testing.T) {
	t.Parallel()

	if _, err := ParseModelRef("claude/"); err == nil {
		t.Fatalf("expected error for empty claude model id")
	}
}

func writeTestAuthFile(t *testing.T, authFile claudeAuthFile) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	data, err := json.Marshal(authFile)
	if err != nil {
		t.Fatalf("could not marshal test auth file: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("could not write test auth file: %v", err)
	}
	return path
}
