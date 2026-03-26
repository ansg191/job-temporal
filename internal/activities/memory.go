package activities

import (
	"context"
	"encoding/json"
	"fmt"

	"go.temporal.io/sdk/temporal"

	"github.com/ansg191/job-temporal/internal/database"
)

type SaveAgentMemoryRequest struct {
	Owner   string
	Repo    string
	Content string
}

func SaveAgentMemory(ctx context.Context, req SaveAgentMemoryRequest) (string, error) {
	if len(req.Content) > 2000 {
		return "", temporal.NewNonRetryableApplicationError(
			"memory content exceeds 2000 character limit",
			"ContentTooLong",
			nil,
		)
	}

	db, err := database.NewPostgresDatabase()
	if err != nil {
		return "", err
	}
	defer db.Close()

	id, err := db.AddMemory(ctx, req.Owner, req.Repo, req.Content)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("Saved memory entry (ID: %d)", id), nil
}

type ListAgentMemoriesRequest struct {
	Owner string
	Repo  string
	Limit int
}

func ListAgentMemories(ctx context.Context, req ListAgentMemoriesRequest) (string, error) {
	db, err := database.NewPostgresDatabase()
	if err != nil {
		return "", err
	}
	defer db.Close()

	if req.Limit <= 0 {
		req.Limit = 50
	}

	entries, err := db.ListMemories(ctx, req.Owner, req.Repo, req.Limit)
	if err != nil {
		return "", err
	}

	bytes, err := json.Marshal(entries)
	if err != nil {
		return "", fmt.Errorf("marshal memories: %w", err)
	}

	return string(bytes), nil
}

type DeleteAgentMemoryRequest struct {
	Owner string
	Repo  string
	ID    int
}

func DeleteAgentMemory(ctx context.Context, req DeleteAgentMemoryRequest) (string, error) {
	db, err := database.NewPostgresDatabase()
	if err != nil {
		return "", err
	}
	defer db.Close()

	deleted, err := db.DeleteMemory(ctx, req.Owner, req.Repo, req.ID)
	if err != nil {
		return "", err
	}

	if deleted {
		return fmt.Sprintf("Deleted memory entry (ID: %d)", req.ID), nil
	}
	return fmt.Sprintf("No memory entry found with ID %d (may belong to a different repository)", req.ID), nil
}
