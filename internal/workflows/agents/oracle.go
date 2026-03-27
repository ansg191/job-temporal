package agents

import (
	"strconv"
	"strings"
	"time"

	"go.temporal.io/sdk/workflow"

	"github.com/ansg191/job-temporal/internal/activities"
	"github.com/ansg191/job-temporal/internal/github"
	"github.com/ansg191/job-temporal/internal/llm"
)

type OracleRequest struct {
	github.ClientOptions
	Branch    string   `json:"branch"`
	Builder   string   `json:"builder"`
	File      string   `json:"file"`
	Label     string   `json:"label"`
	Questions []string `json:"questions"`
}

func OracleWorkflow(ctx workflow.Context, req OracleRequest) (string, error) {
	agentCfg, err := loadAgentConfig(ctx, "oracle")
	if err != nil {
		return "", err
	}

	renderCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 3 * time.Minute,
	})
	analyzeCtx := withCallAIActivityOptions(ctx)

	var renderedPages []activities.OracleRenderedPage
	err = workflow.ExecuteActivity(renderCtx, activities.RenderOraclePages, activities.OracleRenderRequest{
		ClientOptions: req.ClientOptions,
		Branch:        req.Branch,
		Builder:       req.Builder,
		File:          req.File,
		Label:         req.Label,
	}).Get(ctx, &renderedPages)
	if err != nil {
		return "", err
	}

	content := []llm.ContentPart{
		llm.TextPart(buildOraclePrompt(req.Questions, len(renderedPages) > 1)),
	}
	for _, page := range renderedPages {
		content = append(content, llm.ImageURLPart(page.URL))
	}

	input := []llm.Message{
		systemMessage(agentCfg.Instructions),
		userMessageParts(content),
	}

	var result activities.AIResponse
	err = workflow.ExecuteActivity(
		analyzeCtx,
		activities.CallAI,
		activities.AIRequest{
			Model:       agentCfg.Model,
			Input:       input,
			Temperature: temperatureOpt(agentCfg.Temperature),
		},
	).Get(ctx, &result)
	if err != nil {
		return "", err
	}

	for _, page := range renderedPages {
		_ = workflow.ExecuteActivity(
			renderCtx,
			activities.DeletePDFByURL,
			activities.DeletePDFByURLRequest{URL: page.URL},
		).Get(ctx, nil)
	}

	return result.OutputText, nil
}

func buildOraclePrompt(questions []string, multipleImages bool) string {
	var b strings.Builder
	b.WriteString("Answer the following questions about this cropped section of the resume:\n\n")
	for i, question := range questions {
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString(". ")
		b.WriteString(question)
		b.WriteByte('\n')
	}
	if multipleImages {
		b.WriteByte('\n')
		b.WriteString("These images show consecutive pages of the same section.\n")
	}
	return b.String()
}
