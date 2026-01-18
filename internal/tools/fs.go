package tools

import (
	"encoding/json"
	"fmt"

	"github.com/openai/openai-go/v3"

	"github.com/ansg191/job-temporal/internal/activities"
)

type readToolArgs struct {
	File string `json:"file"`
}

// ReadToolParseArgs parses the arguments for the read_file tool into the
// existing req ReadFileRequest struct.
func ReadToolParseArgs(args string, req *activities.ReadFileRequest) error {
	var toolArgs readToolArgs
	err := json.Unmarshal([]byte(args), &toolArgs)
	if err != nil {
		return fmt.Errorf("failed to unmarshal read_file tool args: %w", err)
	}

	req.Path = toolArgs.File
	return nil
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
				"required":             []string{"file"},
				"additionalProperties": false,
			},
		},
	},
}

type editToolArgs struct {
	File    string `json:"file"`
	Patch   string `json:"patch"`
	Message string `json:"message"`
}

// EditToolParseArgs parses the arguments for the read_file tool into the
// existing req ReadFileRequest struct.
func EditToolParseArgs(args string, req *activities.EditFileRequest) error {
	var toolArgs editToolArgs
	err := json.Unmarshal([]byte(args), &toolArgs)
	if err != nil {
		return fmt.Errorf("failed to unmarshal read_file tool args: %w", err)
	}

	req.Path = toolArgs.File
	req.Patch = toolArgs.Patch
	req.Message = toolArgs.Message
	return nil
}

var EditFileToolDesc = openai.ChatCompletionToolUnionParam{
	OfFunction: &openai.ChatCompletionFunctionToolParam{
		Function: openai.FunctionDefinitionParam{
			Name:        "edit_file",
			Strict:      openai.Bool(true),
			Description: openai.String("Edit the contents of a file in the current directory"),
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]interface{}{
					"file": map[string]string{
						"type":        "string",
						"description": "The path to the file to edit",
					},
					"patch": map[string]string{
						"type":        "string",
						"description": "A unified diff patch in git diff format. Must include file headers (--- a/file, +++ b/file) and hunk headers (@@ -start,count +start,count @@) followed by context and change lines.",
					},
					"message": map[string]string{
						"type":        "string",
						"description": "The commit message for this edit",
					},
				},
				"required":             []string{"file", "patch", "message"},
				"additionalProperties": false,
			},
		},
	},
}
