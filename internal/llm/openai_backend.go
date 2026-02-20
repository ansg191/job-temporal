package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/conversations"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
)

const aiPollInterval = 2 * time.Second
const aiPollRequestTimeout = 5 * time.Second

type openAIBackend struct{}

func (b *openAIBackend) CreateConversation(ctx context.Context, req ConversationRequest) (*ConversationState, error) {
	client := openai.NewClient(option.WithMaxRetries(0))
	items, err := openAIInputFromMessages(req.Items)
	if err != nil {
		return nil, err
	}
	params := conversations.ConversationNewParams{Items: items}

	conversation, err := client.Conversations.New(ctx, params)
	if err != nil {
		return nil, ClassifyOpenAIError(err)
	}

	return &ConversationState{
		Backend:              string(BackendOpenAI),
		Provider:             string(BackendOpenAI),
		OpenAIConversationID: conversation.ID,
	}, nil
}

func (b *openAIBackend) Generate(ctx context.Context, req Request) (*Response, error) {
	client := openai.NewClient(option.WithMaxRetries(0))
	input, err := openAIInputFromMessages(req.Messages)
	if err != nil {
		return nil, err
	}
	tools := openAIToolsFromCanonical(req.Tools)
	params := responses.ResponseNewParams{
		Input:      responses.ResponseNewParamsInputUnion{OfInputItemList: input},
		Model:      req.Model,
		Tools:      tools,
		Store:      openai.Bool(false),
		Background: openai.Bool(true),
	}
	if req.Temperature != nil {
		params.Temperature = openai.Float(*req.Temperature)
	}

	var state *ConversationState
	if req.Conversation != nil {
		cloned := req.Conversation.Clone()
		state = &cloned
	}
	if state == nil {
		state = &ConversationState{
			Backend:  string(BackendOpenAI),
			Provider: string(BackendOpenAI),
		}
	}
	if state.Backend != "" && state.Backend != string(BackendOpenAI) {
		return nil, NewConfigError("openai backend cannot use conversation backend %q", state.Backend)
	}
	if state.Provider != "" && state.Provider != string(BackendOpenAI) {
		return nil, NewConfigError("openai backend cannot use conversation provider %q", state.Provider)
	}
	state.Backend = string(BackendOpenAI)
	state.Provider = string(BackendOpenAI)

	if state.OpenAIConversationID != "" {
		params.Conversation = responses.ResponseNewParamsConversationUnion{
			OfConversationObject: &responses.ResponseConversationParam{ID: state.OpenAIConversationID},
		}
		params.Truncation = responses.ResponseNewParamsTruncationAuto
	}

	if req.Instructions != "" {
		params.Instructions = openai.String(req.Instructions)
	}
	if req.Text != nil {
		schemaMap, err := toSchemaMap(req.Text.Schema)
		if err != nil {
			return nil, err
		}
		params.Text = responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigUnionParam{
				OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
					Name:   req.Text.Name,
					Schema: schemaMap,
					Strict: openai.Bool(req.Text.Strict),
				},
			},
		}
	}

	var resp *responses.Response
	err = withActivityHeartbeat(
		ctx,
		providerHeartbeatInterval,
		func() any {
			return map[string]any{
				"backend":  string(BackendOpenAI),
				"provider": string(BackendOpenAI),
				"model":    req.Model,
				"phase":    "create_response",
			}
		},
		func() error {
			var callErr error
			resp, callErr = client.Responses.New(ctx, params, OpenAIContextManagementOptions(req.Model)...)
			return callErr
		},
	)
	if err != nil {
		return nil, ClassifyOpenAIError(err)
	}

	resp, err = waitForBackgroundResponse(ctx, client, resp)
	if err != nil {
		return nil, err
	}

	toolCalls := make([]ToolCall, 0)
	for _, item := range resp.Output {
		if item.Type == "function_call" {
			toolCalls = append(toolCalls, ToolCall{CallID: item.CallID, Name: item.Name, Arguments: item.Arguments})
		}
	}

	return &Response{
		OutputText:   resp.OutputText(),
		ToolCalls:    toolCalls,
		Conversation: state,
	}, nil
}

func openAIInputFromMessages(messages []Message) (responses.ResponseInputParam, error) {
	input := make(responses.ResponseInputParam, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case RoleSystem, RoleUser, RoleAssistant:
			content, err := openAIContentFromParts(msg.Content)
			if err != nil {
				return nil, err
			}
			if len(content) == 0 {
				content = append(content, responses.ResponseInputContentParamOfInputText(""))
			}
			var role responses.EasyInputMessageRole
			switch msg.Role {
			case RoleSystem:
				role = responses.EasyInputMessageRoleSystem
			case RoleAssistant:
				role = responses.EasyInputMessageRoleAssistant
			default:
				role = responses.EasyInputMessageRoleUser
			}
			item := responses.ResponseInputItemParamOfMessage(content, role)
			if item.OfMessage != nil {
				item.OfMessage.Type = responses.EasyInputMessageTypeMessage
			}
			input = append(input, item)
		case RoleTool:
			input = append(input, responses.ResponseInputItemUnionParam{
				OfFunctionCallOutput: &responses.ResponseInputItemFunctionCallOutputParam{
					CallID: msg.ToolCallID,
					Output: responses.ResponseInputItemFunctionCallOutputOutputUnionParam{OfString: openai.String(msg.Text())},
				},
			})
		default:
			return nil, fmt.Errorf("unsupported openai message role %q", msg.Role)
		}
	}
	return input, nil
}

func openAIContentFromParts(parts []ContentPart) (responses.ResponseInputMessageContentListParam, error) {
	ret := make(responses.ResponseInputMessageContentListParam, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case ContentTypeText:
			ret = append(ret, responses.ResponseInputContentParamOfInputText(part.Text))
		case ContentTypeImageURL:
			detail := responses.ResponseInputImageDetailHigh
			switch strings.ToLower(strings.TrimSpace(part.ImageDetail)) {
			case "low":
				detail = responses.ResponseInputImageDetailLow
			case "auto":
				detail = responses.ResponseInputImageDetailAuto
			}
			image := responses.ResponseInputContentParamOfInputImage(detail)
			image.OfInputImage.ImageURL = param.NewOpt(part.ImageURL)
			ret = append(ret, image)
		default:
			return nil, fmt.Errorf("unsupported openai content type %q", part.Type)
		}
	}
	return ret, nil
}

func openAIToolsFromCanonical(tools []ToolDefinition) []responses.ToolUnionParam {
	ret := make([]responses.ToolUnionParam, 0, len(tools))
	for _, tool := range tools {
		fn := responses.FunctionToolParam{
			Name:       tool.Name,
			Parameters: tool.Parameters,
			Strict:     openai.Bool(tool.Strict),
		}
		if tool.Description != "" {
			fn.Description = openai.String(tool.Description)
		}
		ret = append(ret, responses.ToolUnionParam{OfFunction: &fn})
	}
	return ret
}

func waitForBackgroundResponse(ctx context.Context, client openai.Client, resp *responses.Response) (*responses.Response, error) {
	if resp == nil {
		return nil, fmt.Errorf("openai background response is nil")
	}

	responseID := resp.ID
	if responseID == "" {
		return nil, fmt.Errorf("openai background response missing id")
	}

	ticker := time.NewTicker(aiPollInterval)
	defer ticker.Stop()
	var err error

	for {
		if resp != nil {
			activity.RecordHeartbeat(ctx, map[string]any{
				"response_id": responseID,
				"status":      string(resp.Status),
			})

			switch resp.Status {
			case responses.ResponseStatusCompleted:
				return resp, nil
			case responses.ResponseStatusFailed, responses.ResponseStatusCancelled, responses.ResponseStatusIncomplete:
				return nil, temporal.NewApplicationError(
					fmt.Sprintf("openai background response ended with status %q", resp.Status),
					"OpenAIBackgroundResponseError",
					resp.Status,
					resp.IncompleteDetails,
					resp.Error,
				)
			}
		}

		select {
		case <-ctx.Done():
			cancelOpenAIBackgroundResponse(responseID, client)
			return nil, ctx.Err()
		case <-ticker.C:
		}

		pollCtx, cancel := context.WithTimeout(ctx, aiPollRequestTimeout)
		resp, err = client.Responses.Get(pollCtx, responseID, responses.ResponseGetParams{})
		cancel()
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				if parentErr := ctx.Err(); parentErr != nil {
					cancelOpenAIBackgroundResponse(responseID, client)
					return nil, parentErr
				}
			}

			if isTransientPollError(err) {
				slog.Warn("openai background poll failed, retrying", "response_id", responseID, "error", err)
				continue
			}

			return nil, ClassifyOpenAIError(err)
		}
	}
}

func isTransientPollError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	var apiErr *openai.Error
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == 429 || apiErr.StatusCode >= 500
	}

	return false
}

func cancelOpenAIBackgroundResponse(responseID string, client openai.Client) {
	if responseID == "" {
		return
	}

	cancelCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := client.Responses.Cancel(cancelCtx, responseID); err != nil {
		slog.Warn("failed to cancel openai background response", "response_id", responseID, "error", err)
	}
}

func ClassifyOpenAIError(err error) error {
	var apiErr *openai.Error
	if errors.As(err, &apiErr) && apiErr.StatusCode == 400 {
		if isRetryableURLDownloadTimeout(apiErr) {
			return err
		}

		msg := "openai invalid request"
		if apiErr.Param != "" {
			msg += " (param: " + apiErr.Param + ")"
		}
		if apiErr.Message != "" {
			msg += ": " + apiErr.Message
		}
		return temporal.NewNonRetryableApplicationError(msg, "OpenAIInvalidRequestError", err)
	}
	return err
}

func isRetryableURLDownloadTimeout(apiErr *openai.Error) bool {
	if apiErr == nil || apiErr.Param != "url" {
		return false
	}
	return strings.Contains(strings.ToLower(apiErr.Message), "timeout while downloading")
}

func OpenAIContextManagementOptions(model string) []option.RequestOption {
	contextWindow, ok := OpenAIModelContextWindow(model)
	if !ok {
		return nil
	}

	threshold := contextWindow / 2
	return []option.RequestOption{
		option.WithJSONSet("context_management", []map[string]any{
			{
				"type":              "compaction",
				"compact_threshold": threshold,
			},
		}),
	}
}

func OpenAIModelContextWindow(model string) (int64, bool) {
	switch model {
	case openai.ChatModelGPT5_2,
		openai.ChatModelGPT5_2_2025_12_11,
		openai.ChatModelGPT5_2Pro,
		openai.ChatModelGPT5_2Pro2025_12_11:
		return 400_000, true
	case openai.ChatModelGPT5_2ChatLatest:
		return 128_000, true
	default:
		return 0, false
	}
}

func toSchemaMap(schema any) (map[string]any, error) {
	if m, ok := schema.(map[string]any); ok {
		return m, nil
	}

	data, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}

	var m map[string]any
	if err = json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}
