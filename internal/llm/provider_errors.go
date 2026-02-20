package llm

import (
	"errors"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"go.temporal.io/sdk/temporal"
)

func ClassifyAnthropicError(err error) error {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) && apiErr.StatusCode == 400 {
		msg := "anthropic invalid request"
		if apiErr.RequestID != "" {
			msg += " (request_id: " + apiErr.RequestID + ")"
		}
		if raw := strings.TrimSpace(apiErr.RawJSON()); raw != "" {
			msg += ": " + raw
		}
		return temporal.NewNonRetryableApplicationError(
			msg,
			"AnthropicInvalidRequestError",
			err,
		)
	}
	return err
}

func ClassifyProviderError(provider string, err error) error {
	switch provider {
	case string(BackendOpenAI):
		return ClassifyOpenAIError(err)
	case string(BackendAnthropic):
		return ClassifyAnthropicError(err)
	default:
		return err
	}
}
