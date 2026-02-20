package tools

import "github.com/ansg191/job-temporal/internal/llm"

var BuildToolDesc = llm.ToolDefinition{
	Name:        "build",
	Description: "Perform a compilation build, returning errors if they occur",
}
