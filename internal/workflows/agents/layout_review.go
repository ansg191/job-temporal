package agents

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"go.temporal.io/sdk/workflow"

	"github.com/ansg191/job-temporal/internal/activities"
	"github.com/ansg191/job-temporal/internal/llm"
)

// Deprecated: layoutReviewSystemPrompt is kept for rollback safety. Use GetAgentConfig("layout_review") instead.
const layoutReviewSystemPrompt = `
You are a strict PDF typesetting reviewer for resumes and cover letters.
Review rendered page images and identify only concrete visual/layout defects.

Focus only on things that can be changed by adding or removing text,
not by changing formatting itself.
For example, if a line break causes a single word to be on its own line, that is a defect
which can be fixed by removing a word, adding words, or rewording the sentence.

If you notice an invalid character, that is an OCR issue with special characters, so ignore it.

The reviewer may provide notes in response to previous reviews,
including what they changed and warnings they're ignoring on purpose.
Take this into account.

Rubric:
1) line-break problems: awkward wraps, widows/orphans/runts, broken rhythm.
2) whitespace balance: large empty regions, cramped blocks, uneven spacing.
Note that the page will have 1in margins on all 4 sides.
3) alignment consistency: misaligned bullets, dates, section starts, indents.
4) section density/readability: overly dense paragraphs, weak scanning rhythm.
5) bullet wrapping/hanging indents: wrapped lines not aligned with bullet text.
6) line fullness: paragraphs where the final line does not use up the full width,
resulting in wasted space.

Rules:
- Report only issues that are visually evident in the images.
- Keep evidence specific and actionable.
- Use severity: low, medium, or high.
- Single word runts/widows/orphans must be labeled as high severity.
- Try to maximize line fullness.
- If there are no issues, return an empty issues list and explain briefly in summary.
`

func ReviewPDFLayoutWorkflow(ctx workflow.Context, req activities.ReviewPDFLayoutRequest) (string, error) {
	agentCfg, err := loadAgentConfig(ctx, "layout_review")
	if err != nil {
		return "", err
	}

	renderCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 3 * time.Minute,
	})
	analyzeCtx := withCallAIActivityOptions(ctx)

	var renderedPages []activities.LayoutReviewRenderedPage
	err = workflow.ExecuteActivity(renderCtx, activities.RenderLayoutReviewPages, activities.RenderLayoutReviewPagesRequest{
		ReviewPDFLayoutRequest: req,
	}).Get(ctx, &renderedPages)
	if err != nil {
		return "", err
	}

	reviewResult, err := analyzeLayoutReview(analyzeCtx, agentCfg.Instructions, agentCfg.Model, agentCfg.Temperature, renderedPages, req.Notes)
	if err != nil {
		return "", err
	}

	// Best-effort cleanup of uploaded page images.
	for _, page := range renderedPages {
		_ = workflow.ExecuteActivity(
			renderCtx,
			activities.DeletePDFByURL,
			activities.DeletePDFByURLRequest{URL: page.URL},
		).Get(ctx, nil)
	}

	b, err := json.Marshal(reviewResult)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func analyzeLayoutReview(
	ctx workflow.Context,
	instructions string,
	model string,
	temperature *float64,
	pages []activities.LayoutReviewRenderedPage,
	focus string,
) (*activities.ReviewPDFLayoutOutput, error) {
	if len(pages) == 0 {
		return nil, errors.New("no rendered pages")
	}

	content := []llm.ContentPart{
		llm.TextPart(buildLayoutReviewUserPrompt(pages, focus)),
	}
	for _, page := range pages {
		content = append(content, llm.ImageURLPart(page.URL))
	}

	input := []llm.Message{
		systemMessage(instructions),
		userMessageParts(content),
	}

	var result activities.AIResponse
	err := workflow.ExecuteActivity(
		withCallAIActivityOptions(ctx),
		activities.CallAI,
		activities.AIRequest{
			Model:       model,
			Input:       input,
			Text:        activities.LayoutReviewTextFormat,
			Temperature: temperatureOpt(temperature),
		},
	).Get(ctx, &result)
	if err != nil {
		return nil, err
	}

	var output activities.ReviewPDFLayoutOutput
	if err = json.Unmarshal([]byte(result.OutputText), &output); err != nil {
		return nil, fmt.Errorf("failed to parse layout review output: %w", err)
	}

	output.CheckedPages = make([]int, 0, len(pages))
	for _, page := range pages {
		output.CheckedPages = append(output.CheckedPages, page.Page)
	}

	output.Issues = sanitizeLayoutReviewIssues(output.Issues)
	if len(output.Issues) == 0 {
		output.Issues = []activities.ReviewPDFLayoutIssue{}
	}

	return &output, nil
}

func buildLayoutReviewUserPrompt(pages []activities.LayoutReviewRenderedPage, notes string) string {
	pageNums := make([]string, 0, len(pages))
	for _, page := range pages {
		pageNums = append(pageNums, fmt.Sprintf("%d", page.Page))
	}
	basePrompt := "Review pages " + strings.Join(pageNums, ", ") + " for visual typesetting quality. Return only the required JSON schema."

	notes = strings.TrimSpace(notes)
	if notes == "" {
		return basePrompt
	}
	return basePrompt + " Additional notes: " + notes
}

func sanitizeLayoutReviewIssues(issues []activities.ReviewPDFLayoutIssue) []activities.ReviewPDFLayoutIssue {
	if len(issues) == 0 {
		return nil
	}

	validSeverities := []string{"low", "medium", "high"}
	ret := make([]activities.ReviewPDFLayoutIssue, 0, len(issues))
	for _, issue := range issues {
		issue.Severity = strings.ToLower(strings.TrimSpace(issue.Severity))
		if !slices.Contains(validSeverities, issue.Severity) {
			issue.Severity = "medium"
		}
		issue.IssueType = strings.TrimSpace(issue.IssueType)
		issue.Evidence = strings.TrimSpace(issue.Evidence)
		issue.FixHint = strings.TrimSpace(issue.FixHint)
		if issue.Page <= 0 || issue.IssueType == "" || issue.Evidence == "" || issue.FixHint == "" {
			continue
		}
		ret = append(ret, issue)
	}
	return ret
}
