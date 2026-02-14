package activities

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/conversations"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
)

// ResponseTextFormat is a Temporal-serializable representation of a JSON schema text format.
// OpenAI SDK union types don't serialize properly through Temporal, so we use this intermediate type.
type ResponseTextFormat struct {
	Name   string `json:"name"`
	Schema any    `json:"schema"`
	Strict bool   `json:"strict"`
}

type OpenAIResponsesRequest struct {
	Model          string                       `json:"model"`
	Input          responses.ResponseInputParam `json:"input"`
	Tools          []responses.ToolUnionParam   `json:"tools"`
	Temperature    param.Opt[float64]           `json:"temperature"`
	Text           *ResponseTextFormat          `json:"text,omitempty"`
	Instructions   string                       `json:"instructions,omitempty"`
	ConversationID string                       `json:"conversation_id,omitempty"`
}

type OpenAIConversationRequest struct {
	Items responses.ResponseInputParam `json:"items,omitempty"`
}

const aiPollInterval = 2 * time.Second
const aiPollRequestTimeout = 5 * time.Second

func GenerateTextFormat[T any](name string) *ResponseTextFormat {
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		panic(err)
	}

	return &ResponseTextFormat{
		Name:   name,
		Schema: schema,
		Strict: true,
	}
}

func CallAI(ctx context.Context, request OpenAIResponsesRequest) (*responses.Response, error) {
	client := openai.NewClient(option.WithMaxRetries(0))

	params := responses.ResponseNewParams{
		Input:       responses.ResponseNewParamsInputUnion{OfInputItemList: request.Input},
		Model:       request.Model,
		Tools:       request.Tools,
		Temperature: request.Temperature,
		Store:       openai.Bool(false),
		Background:  openai.Bool(true),
	}
	if request.ConversationID != "" {
		params.Conversation = responses.ResponseNewParamsConversationUnion{
			OfConversationObject: &responses.ResponseConversationParam{
				ID: request.ConversationID,
			},
		}
		params.Truncation = responses.ResponseNewParamsTruncationAuto
	}

	if request.Instructions != "" {
		params.Instructions = openai.String(request.Instructions)
	}

	if request.Text != nil {
		schemaMap, err := toSchemaMap(request.Text.Schema)
		if err != nil {
			return nil, err
		}

		params.Text = responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigUnionParam{
				OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
					Name:   request.Text.Name,
					Schema: schemaMap,
					Strict: openai.Bool(request.Text.Strict),
				},
			},
		}
	}

	resp, err := client.Responses.New(ctx, params, contextManagementOptions(request.Model)...)
	if err != nil {
		return nil, classifyOpenAIError(err)
	}

	return waitForBackgroundResponse(ctx, client, resp)
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
		// Apparently, responses.Get can sometimes return nil, nil
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
			// If the parent activity context is canceled/deadline-exceeded, stop polling
			// and cancel the background response.
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

			return nil, classifyOpenAIError(err)
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

func CreateConversation(ctx context.Context, request OpenAIConversationRequest) (string, error) {
	client := openai.NewClient(option.WithMaxRetries(0))
	params := conversations.ConversationNewParams{
		Items: request.Items,
	}

	conversation, err := client.Conversations.New(ctx, params)
	if err != nil {
		return "", classifyOpenAIError(err)
	}

	return conversation.ID, nil
}

func contextManagementOptions(model string) []option.RequestOption {
	contextWindow, ok := modelContextWindow(model)
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

func classifyOpenAIError(err error) error {
	var apiErr *openai.Error
	if errors.As(err, &apiErr) && apiErr.StatusCode == 400 && apiErr.Type == "invalid_request_error" {
		// Some URL fetch failures are transient (e.g. storage edge/network hiccups)
		// even though OpenAI returns them as 400 invalid_request_error.
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

func modelContextWindow(model string) (int64, bool) {
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

// toSchemaMap converts an arbitrary schema value to map[string]any via a JSON
// marshal/unmarshal round-trip, which handles both already-typed maps and
// struct-based schema representations (e.g. *jsonschema.Schema).
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
