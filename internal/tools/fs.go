package tools

import (
	"encoding/json"
	"fmt"

	"github.com/ansg191/job-temporal/internal/activities"
	"github.com/ansg191/job-temporal/internal/llm"
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

var ReadFileToolDesc = llm.ToolDefinition{
	Name:        "read_file",
	Strict:      true,
	Description: "Read the contents of a file from the current directory",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]interface{}{
			"file": map[string]string{
				"type": "string",
			},
		},
		"required":             []string{"file"},
		"additionalProperties": false,
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

var EditFileToolDesc = llm.ToolDefinition{
	Name:        "edit_file",
	Strict:      true,
	Description: "Edit the contents of a file in the current directory",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]interface{}{
			"file": map[string]string{
				"type":        "string",
				"description": "The path to the file to edit",
			},
			"patch": map[string]string{
				"type": "string",
				"description": `A unified diff patch in git diff format. Structure:
1. File headers: '--- a/file' and '+++ b/file'
2. Hunk header: '@@ -OLD_START,OLD_COUNT +NEW_START,NEW_COUNT @@' where OLD_COUNT is the exact number of lines starting with ' ' or '-', and NEW_COUNT is the exact number of lines starting with ' ' or '+'.
3. Hunk body: Lines prefixed with ' ' (context/unchanged), '-' (removed), or '+' (added).

CRITICAL RULES:
- The counts MUST match the actual line counts in the hunk body. For a single-line change, use '@@ -N +N @@' (omit count when it equals 1).
- Lines prefixed with '-' MUST be copied EXACTLY (character-for-character, including all whitespace) from the file. Do NOT paraphrase or retype from memory. Any mismatch will cause the patch to fail.
- Always read_file first and copy the exact line content for '-' lines.`,
			},
			"message": map[string]string{
				"type":        "string",
				"description": "The commit message for this edit",
			},
		},
		"required":             []string{"file", "patch", "message"},
		"additionalProperties": false,
	},
}

type editLineToolArgs struct {
	File       string `json:"file"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	NewContent string `json:"new_content"`
	Message    string `json:"message"`
}

// EditLineToolParseArgs parses the arguments for the edit_line tool into the
// existing req EditLineRequest struct.
func EditLineToolParseArgs(args string, req *activities.EditLineRequest) error {
	var toolArgs editLineToolArgs
	err := json.Unmarshal([]byte(args), &toolArgs)
	if err != nil {
		return fmt.Errorf("failed to unmarshal edit_line tool args: %w", err)
	}

	req.Path = toolArgs.File
	req.StartLine = toolArgs.StartLine
	req.EndLine = toolArgs.EndLine
	req.NewContent = toolArgs.NewContent
	req.Message = toolArgs.Message
	return nil
}

var EditLineToolDesc = llm.ToolDefinition{
	Name:        "edit_line",
	Strict:      true,
	Description: "Edit lines in a file by specifying line numbers and replacement content",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]interface{}{
			"file": map[string]string{
				"type":        "string",
				"description": "The path to the file to edit",
			},
			"start_line": map[string]string{
				"type":        "integer",
				"description": "Starting line number (1-indexed)",
			},
			"end_line": map[string]string{
				"type":        "integer",
				"description": "Ending line number (1-indexed, inclusive). Use start_line-1 to insert before start_line",
			},
			"new_content": map[string]string{
				"type":        "string",
				"description": "The replacement content. Use empty string to delete lines",
			},
			"message": map[string]string{
				"type":        "string",
				"description": "The commit message for this edit",
			},
		},
		"required":             []string{"file", "start_line", "end_line", "new_content", "message"},
		"additionalProperties": false,
	},
}
