package activities

import (
	"context"

	"github.com/ansg191/job-temporal/internal/tools"
)

func ToolGetName(_ context.Context) (*tools.ToolActivityResult, error) {
	return &tools.ToolActivityResult{
		Success: true,
		Result:  tools.GetName(),
		//Error: "Unable to load name",
	}, nil
}

func ToolReadFile(_ context.Context, workingDir string, allowed []string, file string) (*tools.ToolActivityResult, error) {
	return tools.ReadFile(workingDir, allowed, file)
}
