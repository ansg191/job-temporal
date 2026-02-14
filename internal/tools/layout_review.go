package tools

import (
	"encoding/json"
	"fmt"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

type reviewPDFLayoutToolArgs struct {
	PageStart int    `json:"page_start"`
	PageEnd   int    `json:"page_end"`
	Focus     string `json:"focus"`
}

type ReviewPDFLayoutArgs struct {
	PageStart int
	PageEnd   int
	Notes     string
}

func ReviewPDFLayoutToolParseArgs(args string, req *ReviewPDFLayoutArgs) error {
	if args == "" {
		return nil
	}

	var toolArgs reviewPDFLayoutToolArgs
	err := json.Unmarshal([]byte(args), &toolArgs)
	if err != nil {
		return fmt.Errorf("failed to unmarshal review_pdf_layout tool args: %w", err)
	}

	req.PageStart = toolArgs.PageStart
	req.PageEnd = toolArgs.PageEnd
	req.Notes = toolArgs.Focus
	return nil
}

var ReviewPDFLayoutToolDesc = responses.ToolUnionParam{
	OfFunction: &responses.FunctionToolParam{
		Name:        "review_pdf_layout",
		Strict:      openai.Bool(true),
		Description: openai.String("Render the generated PDF pages and return structured typesetting/layout issues with fixes."),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"page_start": map[string]any{
					"type":        "integer",
					"description": "1-indexed start page to review. Use 1 if unsure.",
					"minimum":     1,
				},
				"page_end": map[string]any{
					"type":        "integer",
					"description": "1-indexed end page to review. Use page_start for a single page.",
					"minimum":     1,
				},
				"notes": map[string]any{
					"type":        "string",
					"description": "Notes for the review workflow. Should be blank initially.\nWhen you fix an issue, explain how you fixed it in the notes.\nIf you purposefully ignored issues, note them and explain why.",
				},
			},
			"required":             []string{"page_start", "page_end", "notes"},
			"additionalProperties": false,
		},
	},
}
