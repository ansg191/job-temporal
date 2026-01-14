package tools

import (
	"github.com/openai/openai-go/v3"
)

func GetName() string {
	return "Anshul"
}

var GetNameToolDesc = openai.ChatCompletionToolUnionParam{
	OfFunction: &openai.ChatCompletionFunctionToolParam{
		Function: openai.FunctionDefinitionParam{
			Name:        "get_name",
			Strict:      openai.Bool(false),
			Description: openai.String("Get the name of the user"),
			Parameters:  nil,
		},
	},
}
