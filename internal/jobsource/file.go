package jobsource

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type FileStrategy struct {
	// baseDir is the root directory under which file:// URLs are allowed.
	// It is resolved to an absolute, cleaned path when the strategy is constructed.
	baseDir string
}

// NewFileStrategy constructs a strategy for file:// URLs.
func NewFileStrategy() *FileStrategy {
	// Allow configuration of the safe base directory via environment variable.
	baseDir := os.Getenv("JOB_FILE_BASE_DIR")
	if strings.TrimSpace(baseDir) == "" {
		// Default to the current working directory if not configured.
		if cwd, err := os.Getwd(); err == nil {
			baseDir = cwd
		} else {
			// As a last resort, fall back to the filesystem root.
			baseDir = string(os.PathSeparator)
		}
	}

	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		// If we cannot resolve the base directory, fall back to root to avoid panics.
		absBase = string(os.PathSeparator)
	}
	absBase = filepath.Clean(absBase)

	return &FileStrategy{baseDir: absBase}
}

func (s *FileStrategy) Name() string {
	return "file"
}

func (s *FileStrategy) Match(u *url.URL) bool {
	return strings.EqualFold(u.Scheme, "file")
}

func (s *FileStrategy) Fetch(_ context.Context, u *url.URL) (string, error) {
	if host := strings.TrimSpace(u.Host); host != "" && !strings.EqualFold(host, "localhost") {
		return "", fmt.Errorf("unsupported file URL host %q", host)
	}

	if u.Path == "" {
		return "", fmt.Errorf("file URL path is empty")
	}

	unescapedPath, err := url.PathUnescape(u.Path)
	if err != nil {
		return "", fmt.Errorf("invalid file URL path: %w", err)
	}

	path := filepath.Clean(filepath.FromSlash(unescapedPath))
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("file URL path must be absolute")
	}

	if s.baseDir == "" {
		return "", fmt.Errorf("file strategy has no base directory configured")
	}

	// Ensure that the requested path is within the configured base directory.
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("invalid file path %q: %w", path, err)
	}
	absPath = filepath.Clean(absPath)

	// Resolve symlinks on the requested path so that a symlink inside baseDir
	// that points outside cannot bypass the containment check.
	realPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", fmt.Errorf("cannot resolve symlinks for file path %q: %w", absPath, err)
	}
	realPath = filepath.Clean(realPath)

	// Also resolve symlinks on the base directory itself.
	realBase, err := filepath.EvalSymlinks(s.baseDir)
	if err != nil {
		return "", fmt.Errorf("cannot resolve symlinks for base directory %q: %w", s.baseDir, err)
	}
	realBase = filepath.Clean(realBase)

	// Enforce that realPath is inside realBase.
	// Trailing separators are added to both sides to prevent a directory named
	// with the same prefix (e.g. /safe/dir-evil vs /safe/dir) from matching.
	base := realBase
	if !strings.HasSuffix(base, string(os.PathSeparator)) {
		base += string(os.PathSeparator)
	}
	pathWithSep := realPath
	if !strings.HasSuffix(pathWithSep, string(os.PathSeparator)) {
		pathWithSep += string(os.PathSeparator)
	}
	if !strings.HasPrefix(pathWithSep, base) {
		return "", fmt.Errorf("file URL path %q is outside the allowed directory", absPath)
	}

	data, err := os.ReadFile(realPath)
	if err != nil {
		return "", fmt.Errorf("read file %q: %w", realPath, err)
	}

	return strings.TrimSpace(string(data)), nil
}
