package github

import (
	"context"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
)

const DefaultGithubURL = "https://api.githubcopilot.com/mcp"

type Tools struct {
	*mcp.ClientSession
}

// NewTools creates a new Tools, connecting to the github MCP located at URL.
func NewTools(ctx context.Context, url string) (*Tools, error) {
	itr, err := getTransport()
	if err != nil {
		return nil, fmt.Errorf("failed to get transport: %w", err)
	}

	httpClient := &http.Client{
		Transport: &bearerTokenTransport{
			itr:  itr,
			http: http.DefaultTransport,
		},
	}

	transport := &mcp.StreamableClientTransport{
		Endpoint:   url,
		HTTPClient: httpClient,
	}

	client := mcp.NewClient(&mcp.Implementation{}, nil)

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, err
	}

	return &Tools{session}, nil
}

// OpenAITools returns all available tools as a responses.ToolUnionParam slice.
func (t *Tools) OpenAITools(ctx context.Context) ([]responses.ToolUnionParam, error) {
	result, err := t.ListTools(ctx, nil)
	if err != nil {
		return nil, err
	}

	tools := make([]responses.ToolUnionParam, 0, len(result.Tools))
	for _, tool := range result.Tools {
		tools = append(tools, mcpToolToOpenAI(tool))
	}
	return tools, nil
}

func mcpToolToOpenAI(tool *mcp.Tool) responses.ToolUnionParam {
	fn := responses.FunctionToolParam{
		Name: tool.Name,
	}

	if tool.Description != "" {
		fn.Description = param.NewOpt(tool.Description)
	}

	if schema, ok := tool.InputSchema.(map[string]any); ok {
		fn.Parameters = schema
	}

	return responses.ToolUnionParam{OfFunction: &fn}
}
