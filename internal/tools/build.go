package tools

import "github.com/openai/openai-go/v3"

var BuildToolDesc = openai.ChatCompletionToolUnionParam{
	OfFunction: &openai.ChatCompletionFunctionToolParam{
		Function: openai.FunctionDefinitionParam{
			Name:        "build",
			Strict:      openai.Bool(false),
			Description: openai.String("Perform a compilation build, returning errors if they occur"),
			Parameters:  nil,
		},
	},
}
