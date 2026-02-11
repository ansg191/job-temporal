package tools

import (
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

var BuildToolDesc = responses.ToolUnionParam{
	OfFunction: &responses.FunctionToolParam{
		Name:        "build",
		Strict:      openai.Bool(false),
		Description: openai.String("Perform a compilation build, returning errors if they occur"),
	},
}
