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

func ReviewLetterContentWorkflow(ctx workflow.Context, req activities.ReviewLetterContentRequest) (string, error) {
	agentCfg, err := loadAgentConfig(ctx, "letter_review")
	if err != nil {
		return "", err
	}

	readCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 3 * time.Minute,
	})

	var letterContent string
	err = workflow.ExecuteActivity(readCtx, activities.ReadLetterContent, activities.ReadLetterContentRequest{
		ClientOptions: req.ClientOptions,
		Branch:        req.Branch,
	}).Get(ctx, &letterContent)
	if err != nil {
		return "", err
	}

	reviewResult, err := analyzeLetterReview(
		withCallAIActivityOptions(ctx),
		agentCfg.Instructions,
		agentCfg.Model,
		agentCfg.Temperature,
		letterContent,
		req.Job,
	)
	if err != nil {
		return "", err
	}

	b, err := json.Marshal(reviewResult)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func analyzeLetterReview(
	ctx workflow.Context,
	instructions string,
	model string,
	temperature *float64,
	letterContent string,
	job string,
) (*activities.ReviewLetterContentOutput, error) {
	if strings.TrimSpace(letterContent) == "" {
		return nil, errors.New("empty letter content")
	}

	prompt := buildLetterReviewUserPrompt(letterContent, job)
	input := []llm.Message{
		systemMessage(instructions),
		userMessage(prompt),
	}

	pendingInput := input
	var conversation *llm.ConversationState
	var output activities.ReviewLetterContentOutput
	for {
		var result activities.AIResponse
		err := workflow.ExecuteActivity(
			withCallAIActivityOptions(ctx),
			activities.CallAI,
			activities.AIRequest{
				Model:        model,
				Input:        pendingInput,
				Text:         activities.LetterReviewTextFormat,
				Temperature:  temperatureOpt(temperature),
				Conversation: conversation,
			},
		).Get(ctx, &result)
		if err != nil {
			return nil, err
		}
		conversation = result.Conversation
		if hasFunctionCalls(result.ToolCalls) {
			return nil, fmt.Errorf("letter review returned unexpected tool calls")
		}
		if aiShouldContinue(result) {
			pendingInput = []llm.Message{userMessage(continuationMessage)}
			continue
		}
		if err = json.Unmarshal([]byte(result.OutputText), &output); err != nil {
			return nil, fmt.Errorf("failed to parse letter review output: %w", err)
		}
		break
	}

	output.Issues = sanitizeLetterReviewIssues(output.Issues)
	if len(output.Issues) == 0 {
		output.Issues = []activities.ReviewLetterContentIssue{}
	}

	return &output, nil
}

func buildLetterReviewUserPrompt(letterContent string, job string) string {
	var sb strings.Builder
	sb.WriteString("Review the following cover letter for content quality. Return only the required JSON schema.\n\n")
	sb.WriteString("<cover_letter>\n")
	sb.WriteString(letterContent)
	sb.WriteString("\n</cover_letter>\n\n")
	sb.WriteString("<job_description>\n")
	sb.WriteString(job)
	sb.WriteString("\n</job_description>")
	return sb.String()
}

func sanitizeLetterReviewIssues(issues []activities.ReviewLetterContentIssue) []activities.ReviewLetterContentIssue {
	if len(issues) == 0 {
		return nil
	}

	validSeverities := []string{"low", "medium", "high"}
	ret := make([]activities.ReviewLetterContentIssue, 0, len(issues))
	for _, issue := range issues {
		issue.Severity = strings.ToLower(strings.TrimSpace(issue.Severity))
		if !slices.Contains(validSeverities, issue.Severity) {
			issue.Severity = "medium"
		}
		issue.IssueType = strings.TrimSpace(issue.IssueType)
		issue.Location = strings.TrimSpace(issue.Location)
		issue.Evidence = strings.TrimSpace(issue.Evidence)
		issue.FixHint = strings.TrimSpace(issue.FixHint)
		if issue.IssueType == "" || issue.Evidence == "" || issue.FixHint == "" {
			continue
		}
		ret = append(ret, issue)
	}
	return ret
}
