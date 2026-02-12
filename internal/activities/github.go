package activities

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/openai/openai-go/v3/responses"
	"go.temporal.io/sdk/temporal"

	"github.com/ansg191/job-temporal/internal/github"
)

const (
	githubToolMaxAttempts      = 6
	githubToolRetryBaseBackoff = 500 * time.Millisecond
	githubToolRetryMaxBackoff  = 8 * time.Second
)

func ListGithubTools(ctx context.Context) ([]responses.ToolUnionParam, error) {
	var aiTools []responses.ToolUnionParam
	err := retryGithubRateLimit(ctx, func() error {
		var err error
		aiTools, err = github.SharedOpenAITools(ctx)
		return err
	})
	if err != nil {
		return nil, err
	}
	return aiTools, nil
}

func CallGithubTool(ctx context.Context, call responses.ResponseOutputItemUnion) (string, error) {
	log.Println(call.Name, call.Arguments)

	args, err := parseArgs(call.Arguments)
	if err != nil {
		return "", temporal.NewNonRetryableApplicationError(
			"failed to unmarshal tool arguments",
			"InvalidToolArgumentsError",
			err,
		)
	}

	var res *mcp.CallToolResult
	err = retryGithubRateLimit(ctx, func() error {
		var err error
		res, err = github.SharedCallTool(ctx, &mcp.CallToolParams{
			Name:      call.Name,
			Arguments: args,
		})
		return err
	})
	if err != nil {
		return "", err
	}

	result := ""
	for _, content := range res.Content {
		c, err := content.MarshalJSON()
		if err != nil {
			return "", err
		}
		result += string(c)
	}

	return result, nil
}

func parseArgs(argStr string) (map[string]any, error) {
	argStr = strings.TrimSpace(argStr)

	// Case 1: arguments are a JSON object already
	var obj map[string]any
	if err := json.Unmarshal([]byte(argStr), &obj); err == nil {
		return obj, nil
	}

	// Case 2: arguments are a JSON string containing JSON
	var inner string
	if err := json.Unmarshal([]byte(argStr), &inner); err != nil {
		return nil, fmt.Errorf("arguments not object or json-string: %w", err)
	}

	if err := json.Unmarshal([]byte(inner), &obj); err != nil {
		return nil, fmt.Errorf("inner string not valid json object: %w", err)
	}

	return obj, nil
}

func retryGithubRateLimit(ctx context.Context, fn func() error) error {
	backoff := githubToolRetryBaseBackoff
	var err error

	for attempt := 1; attempt <= githubToolMaxAttempts; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}
		if !isGithubRateLimitError(err) || attempt == githubToolMaxAttempts {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > githubToolRetryMaxBackoff {
			backoff = githubToolRetryMaxBackoff
		}
	}

	return err
}

func isGithubRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "too many requests") || strings.Contains(msg, "429")
}
