package tools

import "github.com/ansg191/job-temporal/internal/llm"

var ListLabelsToolDesc = llm.ToolDefinition{
	Name:        "list_labels",
	Description: "List available section labels in the resume that can be passed to the oracle() tool.",
}
