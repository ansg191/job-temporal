package github

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
)

const DefaultGithubURL = "https://api.githubcopilot.com/mcp"

type Tools struct {
	*mcp.ClientSession
}

type toolsManager struct {
	url string

	mu      sync.Mutex
	session *mcp.ClientSession

	lastRequestAt time.Time
	minInterval   time.Duration
}

var sharedTools = newToolsManager(DefaultGithubURL)

func newToolsManager(url string) *toolsManager {
	return &toolsManager{
		url:         url,
		minInterval: getMCPMinInterval(),
	}
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

func SharedOpenAITools(ctx context.Context) ([]responses.ToolUnionParam, error) {
	return sharedTools.openAITools(ctx)
}

func SharedCallTool(ctx context.Context, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	return sharedTools.callTool(ctx, params)
}

func (m *toolsManager) openAITools(ctx context.Context) ([]responses.ToolUnionParam, error) {
	result, err := m.listTools(ctx)
	if err != nil {
		return nil, err
	}

	tools := make([]responses.ToolUnionParam, 0, len(result.Tools))
	for _, tool := range result.Tools {
		tools = append(tools, mcpToolToOpenAI(tool))
	}
	return tools, nil
}

func (m *toolsManager) listTools(ctx context.Context) (*mcp.ListToolsResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, err := m.getOrCreateSession(ctx)
	if err != nil {
		return nil, err
	}
	if err = m.waitForRateLimit(ctx); err != nil {
		return nil, err
	}

	result, err := sess.ListTools(ctx, nil)
	if err != nil && isRecoverableSessionError(err) {
		m.resetSession()
		sess, err = m.getOrCreateSession(ctx)
		if err != nil {
			return nil, err
		}
		if err = m.waitForRateLimit(ctx); err != nil {
			return nil, err
		}
		result, err = sess.ListTools(ctx, nil)
	}
	return result, err
}

func (m *toolsManager) callTool(ctx context.Context, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, err := m.getOrCreateSession(ctx)
	if err != nil {
		return nil, err
	}
	if err = m.waitForRateLimit(ctx); err != nil {
		return nil, err
	}

	result, err := sess.CallTool(ctx, params)
	if err != nil && isRecoverableSessionError(err) {
		m.resetSession()
		sess, err = m.getOrCreateSession(ctx)
		if err != nil {
			return nil, err
		}
		if err = m.waitForRateLimit(ctx); err != nil {
			return nil, err
		}
		result, err = sess.CallTool(ctx, params)
	}
	return result, err
}

func (m *toolsManager) getOrCreateSession(ctx context.Context) (*mcp.ClientSession, error) {
	if m.session != nil {
		return m.session, nil
	}

	tools, err := NewTools(ctx, m.url)
	if err != nil {
		return nil, err
	}
	m.session = tools.ClientSession
	return m.session, nil
}

func (m *toolsManager) resetSession() {
	if m.session == nil {
		return
	}
	_ = m.session.Close()
	m.session = nil
}

func isRecoverableSessionError(err error) bool {
	msg := strings.ToLower(err.Error())

	return strings.Contains(msg, "rejected by transport") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "eof") ||
		strings.Contains(msg, "closed")
}

func (m *toolsManager) waitForRateLimit(ctx context.Context) error {
	if m.minInterval <= 0 {
		return nil
	}

	now := time.Now()
	wait := m.minInterval - now.Sub(m.lastRequestAt)
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}

	m.lastRequestAt = time.Now()
	return nil
}

func getMCPMinInterval() time.Duration {
	const defaultMS = 1200

	value := os.Getenv("GITHUB_MCP_MIN_INTERVAL_MS")
	if value == "" {
		return defaultMS * time.Millisecond
	}

	ms, err := strconv.Atoi(value)
	if err != nil || ms < 0 {
		return defaultMS * time.Millisecond
	}
	return time.Duration(ms) * time.Millisecond
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
