package tools

import "github.com/openai/openai-go/v3"

var ListBranchesToolDesc = openai.ChatCompletionToolUnionParam{
	OfFunction: &openai.ChatCompletionFunctionToolParam{
		Function: openai.FunctionDefinitionParam{
			Name:        "list_branches",
			Strict:      openai.Bool(false),
			Description: openai.String("List all branches in the current git repository"),
			Parameters:  nil,
		},
	},
}
