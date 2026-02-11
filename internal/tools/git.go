package tools

import (
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

var ListBranchesToolDesc = responses.ToolUnionParam{
	OfFunction: &responses.FunctionToolParam{
		Name:        "list_branches",
		Strict:      openai.Bool(false),
		Description: openai.String("List all branches in the current git repository"),
	},
}
