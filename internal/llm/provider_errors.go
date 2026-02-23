package llm

import (
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"go.temporal.io/sdk/temporal"
)

func ClassifyAnthropicError(err error) error {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		if apiErr.StatusCode == 400 {
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
		} else if apiErr.StatusCode == 429 {
			// Rate limited
			// See: https://platform.claude.com/docs/en/api/rate-limits
			if apiErr.Response == nil {
				return temporal.NewApplicationErrorWithCause("anthropic rate limited", "AnthropicRateLimitedError", err)
			}
			retryAfter := apiErr.Response.Header.Get("Retry-After")
			if retryAfter == "" {
				return temporal.NewApplicationErrorWithCause("anthropic rate limited", "AnthropicRateLimitedError", err)
			}
			return temporal.NewApplicationErrorWithOptions(
				"anthropic rate limited, retry after "+retryAfter,
				"AnthropicRateLimitedError",
				temporal.ApplicationErrorOptions{
					NonRetryable:   false,
					Cause:          err,
					NextRetryDelay: parseRetryAfter(retryAfter),
				},
			)
		}
	}
	return err
}

func parseRetryAfter(after string) time.Duration {
	seconds, err := strconv.ParseInt(after, 10, 64)
	if err != nil {
		// Not handling date retry-afters for now.
		slog.Error("failed to parse retry-after header", "after", after, "err", err)
		return 0
	}
	return time.Duration(seconds) * time.Second
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
