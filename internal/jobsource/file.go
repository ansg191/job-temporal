package jobsource

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type FileStrategy struct{}

// NewFileStrategy constructs a strategy for file:// URLs.
func NewFileStrategy() *FileStrategy {
	return &FileStrategy{}
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

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read file %q: %w", path, err)
	}

	return strings.TrimSpace(string(data)), nil
}
