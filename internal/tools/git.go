package tools

import "github.com/ansg191/job-temporal/internal/llm"

var ListBranchesToolDesc = llm.ToolDefinition{
	Name:        "list_branches",
	Description: "List all branches in the current git repository",
}
