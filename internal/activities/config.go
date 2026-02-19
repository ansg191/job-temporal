package activities

import (
	"context"

	"github.com/ansg191/job-temporal/internal/config"
)

// GetAgentConfig is a Temporal activity that loads agent configuration.
func GetAgentConfig(ctx context.Context, agentName string) (*config.AgentConfig, error) {
	return config.LoadAgentConfig(agentName)
}
