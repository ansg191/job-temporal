package tools

import (
	"encoding/json"
	"fmt"

	"github.com/ansg191/job-temporal/internal/llm"
)

type saveMemoryToolArgs struct {
	Content string `json:"content"`
}

type SaveMemoryArgs struct {
	Content string
}

func SaveMemoryToolParseArgs(args string, req *SaveMemoryArgs) error {
	if args == "" {
		return fmt.Errorf("save_memory requires a content argument")
	}
	var toolArgs saveMemoryToolArgs
	err := json.Unmarshal([]byte(args), &toolArgs)
	if err != nil {
		return fmt.Errorf("failed to unmarshal save_memory tool args: %w", err)
	}
	req.Content = toolArgs.Content
	return nil
}

var SaveMemoryToolDesc = llm.ToolDefinition{
	Name:        "save_memory",
	Description: "Save a guideline or lesson learned for future builder agents working in this repository. Use this when you identify a pattern, preference, or common mistake that should be remembered for future document builds. The content should be a clear, actionable instruction (max 2000 characters).",
	Strict:      true,
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"content": map[string]any{
				"type":        "string",
				"description": "The guideline or lesson to save for future builder agents. Should be a clear, actionable instruction.",
			},
		},
		"required":             []string{"content"},
		"additionalProperties": false,
	},
}

var ListMemoriesToolDesc = llm.ToolDefinition{
	Name:        "list_memories",
	Description: "List all stored memory entries for the current repository. Returns a JSON array of entries with id, content, and created_at fields. Use this to review what guidelines have been saved.",
}

type deleteMemoryToolArgs struct {
	ID int `json:"id"`
}

type DeleteMemoryArgs struct {
	ID int
}

func DeleteMemoryToolParseArgs(args string, req *DeleteMemoryArgs) error {
	if args == "" {
		return fmt.Errorf("delete_memory requires an id argument")
	}
	var toolArgs deleteMemoryToolArgs
	err := json.Unmarshal([]byte(args), &toolArgs)
	if err != nil {
		return fmt.Errorf("failed to unmarshal delete_memory tool args: %w", err)
	}
	req.ID = toolArgs.ID
	return nil
}

var DeleteMemoryToolDesc = llm.ToolDefinition{
	Name:        "delete_memory",
	Description: "Delete a memory entry by its ID. Use this to remove outdated or incorrect guidelines.",
	Strict:      true,
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{
				"type":        "integer",
				"description": "The ID of the memory entry to delete.",
			},
		},
		"required":             []string{"id"},
		"additionalProperties": false,
	},
}
