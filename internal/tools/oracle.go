package tools

import (
	"encoding/json"
	"fmt"

	"github.com/ansg191/job-temporal/internal/llm"
)

type oracleToolArgs struct {
	Label     string   `json:"label"`
	Questions []string `json:"questions"`
}

type OracleArgs struct {
	Label     string
	Questions []string
}

func OracleToolParseArgs(args string, req *OracleArgs) error {
	var toolArgs oracleToolArgs
	err := json.Unmarshal([]byte(args), &toolArgs)
	if err != nil {
		return fmt.Errorf("failed to unmarshal oracle tool args: %w", err)
	}

	if toolArgs.Label == "" {
		return fmt.Errorf("oracle tool label is required")
	}
	if len(toolArgs.Questions) == 0 {
		return fmt.Errorf("oracle tool questions are required")
	}

	req.Label = toolArgs.Label
	req.Questions = toolArgs.Questions
	return nil
}

var OracleToolDesc = llm.ToolDefinition{
	Name:        "oracle",
	Description: "Render a cropped section of the resume PDF and answer visual layout questions about it. Pass a section label (from list_labels) and an array of specific visual questions. Returns free-text answers based on the rendered section.",
	Strict:      true,
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"label": map[string]any{
				"type":        "string",
				"description": "Section label returned by list_labels.",
			},
			"questions": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Specific visual questions to answer about the rendered section.",
			},
		},
		"required":             []string{"label", "questions"},
		"additionalProperties": false,
	},
}
