package jobsource

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"golang.org/x/net/html"
)

const linkedInGuestEndpoint = "https://www.linkedin.com/jobs-guest/jobs/api/jobPosting/%s"

var (
	errNoLinkedInJobID      = errors.New("unable to extract LinkedIn job ID from URL")
	numericStringPattern    = regexp.MustCompile(`^\d+$`)
	trailingDigitsPattern   = regexp.MustCompile(`(\d+)$`)
	multiSpacePattern       = regexp.MustCompile(`[ \t]+`)
	consecutiveNewlineRegex = regexp.MustCompile(`\n{3,}`)
	spaceBeforeNewlineRegex = regexp.MustCompile(`[ \t]+\n`)
)

type LinkedInStrategy struct {
	client         *http.Client
	endpointFormat string
}

// NewLinkedInStrategy constructs a strategy that resolves LinkedIn job URLs
// via the public jobs-guest endpoint.
func NewLinkedInStrategy(client *http.Client) *LinkedInStrategy {
	if client == nil {
		client = http.DefaultClient
	}
	return &LinkedInStrategy{
		client:         client,
		endpointFormat: linkedInGuestEndpoint,
	}
}

func (s *LinkedInStrategy) Name() string {
	return "linkedin"
}

func (s *LinkedInStrategy) Match(u *url.URL) bool {
	host := strings.ToLower(u.Hostname())
	return host == "linkedin.com" || host == "www.linkedin.com"
}

func (s *LinkedInStrategy) Fetch(ctx context.Context, u *url.URL) (string, error) {
	jobID, err := extractLinkedInJobID(u)
	if err != nil {
		return "", err
	}

	endpoint := fmt.Sprintf(s.endpointFormat, jobID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; job-temporal/1.0)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch endpoint %q: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("linkedin returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	desc, err := parseLinkedInDescription(resp.Body)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(desc) == "" {
		return "", fmt.Errorf("linkedin description not found")
	}

	return desc, nil
}

func extractLinkedInJobID(u *url.URL) (string, error) {
	if currentJobID := strings.TrimSpace(u.Query().Get("currentJobId")); currentJobID != "" && numericStringPattern.MatchString(currentJobID) {
		return currentJobID, nil
	}

	cleanPath := path.Clean(u.Path)
	for _, segment := range strings.Split(strings.Trim(cleanPath, "/"), "/") {
		if numericStringPattern.MatchString(segment) {
			return segment, nil
		}
		if matches := trailingDigitsPattern.FindStringSubmatch(segment); len(matches) == 2 {
			return matches[1], nil
		}
	}

	return "", errNoLinkedInJobID
}

func parseLinkedInDescription(r io.Reader) (string, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return "", fmt.Errorf("parse linkedin html: %w", err)
	}

	target := findNodeWithClass(doc, "show-more-less-html__markup")
	if target == nil {
		target = findNodeWithClass(doc, "description__text")
	}
	if target == nil {
		return "", fmt.Errorf("description section not found")
	}

	markdown, err := htmltomarkdown.ConvertNode(target)
	if err != nil {
		return "", fmt.Errorf("convert linkedin html to markdown: %w", err)
	}

	return normalizeMarkdown(string(markdown)), nil
}

func findNodeWithClass(root *html.Node, classToken string) *html.Node {
	var walk func(n *html.Node) *html.Node
	walk = func(n *html.Node) *html.Node {
		if n.Type == html.ElementNode && hasClass(n, classToken) {
			return n
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if found := walk(child); found != nil {
				return found
			}
		}
		return nil
	}
	return walk(root)
}

func hasClass(n *html.Node, classToken string) bool {
	for _, attr := range n.Attr {
		if attr.Key != "class" {
			continue
		}
		for _, token := range strings.Fields(attr.Val) {
			if token == classToken {
				return true
			}
		}
	}
	return false
}

func normalizeMarkdown(raw string) string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	raw = spaceBeforeNewlineRegex.ReplaceAllString(raw, "\n")
	raw = consecutiveNewlineRegex.ReplaceAllString(raw, "\n\n")

	lines := strings.Split(raw, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}
