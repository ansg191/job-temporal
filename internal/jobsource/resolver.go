package jobsource

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Strategy interface {
	// Name returns a short source identifier (for example "linkedin" or "file").
	// It is used in resolver errors and logs to make failures attributable.
	Name() string
	// Match reports whether this strategy can resolve a given URL.
	Match(u *url.URL) bool
	// Fetch resolves a job description from the provided URL.
	Fetch(ctx context.Context, u *url.URL) (string, error)
}

// Resolver picks a matching job-source strategy for URL inputs.
type Resolver struct {
	strategies []Strategy
}

// NewResolver builds a Resolver with the provided strategies in order.
func NewResolver(strategies ...Strategy) *Resolver {
	return &Resolver{strategies: strategies}
}

// NewDefaultResolver returns the built-in strategy set.
func NewDefaultResolver() *Resolver {
	httpClient := &http.Client{
		Timeout: 20 * time.Second,
	}
	return NewResolver(
		NewFileStrategy(),
		NewLinkedInStrategy(httpClient),
	)
}

// Resolve converts user input to a job description.
// Input must be an absolute URL supported by one of the configured strategies.
func (r *Resolver) Resolve(ctx context.Context, input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", fmt.Errorf("job description input is empty")
	}

	u, err := parseAbsoluteURL(trimmed)
	if err != nil {
		return "", fmt.Errorf("input must be a supported job URL: %w", err)
	}

	for _, strategy := range r.strategies {
		if !strategy.Match(u) {
			continue
		}
		jobDesc, err := strategy.Fetch(ctx, u)
		if err != nil {
			return "", fmt.Errorf("%s strategy failed: %w", strategy.Name(), err)
		}
		if strings.TrimSpace(jobDesc) == "" {
			return "", fmt.Errorf("%s strategy returned an empty job description", strategy.Name())
		}
		return jobDesc, nil
	}

	return "", fmt.Errorf("no strategy available for URL host %q", u.Host)
}

func parseAbsoluteURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "" {
		return nil, fmt.Errorf("not an absolute URL")
	}
	if u.Scheme != "file" && u.Host == "" {
		return nil, fmt.Errorf("not an absolute URL")
	}
	return u, nil
}
