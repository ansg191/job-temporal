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
