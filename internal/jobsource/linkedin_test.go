package jobsource

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestExtractLinkedInJobID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		rawURL  string
		want    string
		wantErr bool
	}{
		{
			name:   "currentJobId query param",
			rawURL: "https://www.linkedin.com/jobs/collections/recommended/?currentJobId=4362653718",
			want:   "4362653718",
		},
		{
			name:   "view path with slug suffix id",
			rawURL: "https://www.linkedin.com/jobs/view/new-college-graduate-4362653718",
			want:   "4362653718",
		},
		{
			name:   "view path with numeric segment",
			rawURL: "https://www.linkedin.com/jobs/view/4362653718/",
			want:   "4362653718",
		},
		{
			name:    "missing id",
			rawURL:  "https://www.linkedin.com/jobs/search/?keywords=go",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			u, err := url.Parse(tt.rawURL)
			if err != nil {
				t.Fatalf("parse URL: %v", err)
			}

			got, err := extractLinkedInJobID(u)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("extractLinkedInJobID returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("extractLinkedInJobID = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseLinkedInDescription(t *testing.T) {
	t.Parallel()

	html := `
<html><body>
	<h1 class="topcard__title">Software Engineer</h1>
	<a class="topcard__org-name-link">Acme Corp</a>
	<ul class="description__job-criteria-list">
		<li class="description__job-criteria-item">
			<h3 class="description__job-criteria-subheader">Seniority level</h3>
			<span class="description__job-criteria-text">Entry level</span>
		</li>
	</ul>
	<div class="description__text description__text--rich">
		<section class="show-more-less-html">
			<div class="show-more-less-html__markup show-more-less-html__markup--clamp-after-5">
				<p>Build backend services in Go.</p>
				<ul>
					<li>Own APIs</li>
					<li>Work with product teams</li>
				</ul>
			</div>
		</section>
	</div>
</body></html>`

	got, err := parseLinkedInDescription(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parseLinkedInDescription returned error: %v", err)
	}

	wantContains := []string{
		"# Acme Corp",
		"## Software Engineer",
		"**Seniority level**: Entry level",
		"Build backend services in Go.",
		"- Own APIs",
		"- Work with product teams",
	}
	for _, want := range wantContains {
		if !strings.Contains(got, want) {
			t.Fatalf("description missing %q in %q", want, got)
		}
	}
}

func TestParseLinkedInDescriptionFromFile(t *testing.T) {
	t.Parallel()

	f, err := os.Open("testdata/linkedin_4362930940.html")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()

	got, err := parseLinkedInDescription(f)
	if err != nil {
		t.Fatalf("parseLinkedInDescription returned error: %v", err)
	}

	wantContains := []string{
		"# Cyngn",
		"## Full Stack Software Engineer",
		"**Seniority level**: Entry level",
		"**Employment type**: Full-time",
		"**Job function**: Engineering and Information Technology",
		"**Industries**: Automation Machinery Manufacturing",
		"About Cyngn",
	}
	for _, want := range wantContains {
		if !strings.Contains(got, want) {
			t.Fatalf("description missing %q in:\n%s", want, got)
		}
	}
}

func TestLinkedInFetch(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/jobs-guest/jobs/api/jobPosting/4362930940" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.ServeFile(w, r, "testdata/linkedin_4362930940.html")
	}))
	defer server.Close()

	strategy := NewLinkedInStrategy(server.Client())
	strategy.endpointFormat = server.URL + "/jobs-guest/jobs/api/jobPosting/%s"

	u, err := url.Parse("https://www.linkedin.com/jobs/view/full-stack-software-engineer-at-cyngn-4362930940")
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}

	desc, err := strategy.Fetch(context.Background(), u)
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	wantContains := []string{
		"# Cyngn",
		"## Full Stack Software Engineer",
		"About Cyngn",
	}
	for _, want := range wantContains {
		if !strings.Contains(desc, want) {
			t.Fatalf("description missing %q in:\n%s", want, desc)
		}
	}
}
