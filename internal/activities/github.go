package activities

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/openai/openai-go/v3"
	"go.temporal.io/sdk/temporal"

	"github.com/ansg191/job-temporal/internal/github"
)

func ListGithubTools(ctx context.Context) ([]openai.ChatCompletionToolUnionParam, error) {
	gh, err := github.NewTools(ctx, github.DefaultGithubURL)
	if err != nil {
		return nil, err
	}
	defer gh.Close()

	return gh.OpenAITools(ctx)
}

func CallGithubTool(ctx context.Context, call openai.ChatCompletionMessageToolCallUnion) (string, error) {
	gh, err := github.NewTools(ctx, github.DefaultGithubURL)
	if err != nil {
		return "", err
	}
	defer gh.Close()

	log.Println(call.Function.Name, call.Function.Arguments)

	args, err := parseArgs(call.Function.Arguments)
	if err != nil {
		return "", temporal.NewNonRetryableApplicationError(
			"failed to unmarshal tool arguments",
			"InvalidToolArgumentsError",
			err,
		)
	}

	res, err := gh.CallTool(ctx, &mcp.CallToolParams{
		Name:      call.Function.Name,
		Arguments: args,
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
