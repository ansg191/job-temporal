package agents

import (
	"strings"
	"unicode"

	"go.temporal.io/sdk/workflow"
)

const maxAgentWorkflowIDComponentLength = 64

// MakeChildWorkflowID builds a readable child workflow ID from the current
// workflow ID and optional semantic parts such as agent/purpose/branch.
func MakeChildWorkflowID(ctx workflow.Context, parts ...string) string {
	parentWorkflowID := workflow.GetInfo(ctx).WorkflowExecution.ID
	components := []string{sanitizeAgentWorkflowIDComponent(parentWorkflowID)}
	for _, part := range parts {
		if clean := sanitizeAgentWorkflowIDComponent(part); clean != "" {
			components = append(components, clean)
		}
	}

	return strings.Join(components, "-")
}

func sanitizeAgentWorkflowIDComponent(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return ""
	}

	var b strings.Builder
	prevDash := false
	for _, r := range v {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			prevDash = false
		case !prevDash:
			b.WriteByte('-')
			prevDash = true
		}
	}

	clean := strings.Trim(b.String(), "-")
	if len(clean) > maxAgentWorkflowIDComponentLength {
		clean = strings.Trim(clean[:maxAgentWorkflowIDComponentLength], "-")
	}
	return clean
}
