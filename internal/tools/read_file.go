package tools

import (
	"fmt"
	"os"
	"path"
	"slices"

	"github.com/openai/openai-go/v3"
)

func ReadFile(
	workingDir string,
	allowed []string,
	file string,
) (*ToolActivityResult, error) {
	if !slices.Contains(allowed, file) {
		return &ToolActivityResult{Success: false, Error: "file not allowed to be read"}, nil
	}

	// Read the file
	filePath := path.Join(workingDir, file)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	return &ToolActivityResult{
		Success: true,
		Result:  string(data),
	}, nil
}

var ReadFileToolDesc = openai.ChatCompletionToolUnionParam{
	OfFunction: &openai.ChatCompletionFunctionToolParam{
		Function: openai.FunctionDefinitionParam{
			Name:        "read_file",
			Strict:      openai.Bool(true),
			Description: openai.String("Read the contents of a file from the current directory"),
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]interface{}{
					"file": map[string]string{
						"type": "string",
					},
				},
				"required": []string{"file"},
			},
		},
	},
}
