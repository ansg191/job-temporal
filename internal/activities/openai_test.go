package activities

import (
	"errors"
	"testing"

	"github.com/openai/openai-go/v3"
	"go.temporal.io/sdk/temporal"
)

func TestModelContextWindow(t *testing.T) {
	t.Parallel()

	window, ok := modelContextWindow(openai.ChatModelGPT5_2)
	if !ok {
		t.Fatalf("expected model to be supported")
	}
	if window != 400_000 {
		t.Fatalf("expected 400000, got %d", window)
	}

	_, ok = modelContextWindow("unknown-model")
	if ok {
		t.Fatalf("expected unknown model to be unsupported")
	}
}

func TestContextManagementOptions(t *testing.T) {
	t.Parallel()

	opts := contextManagementOptions(openai.ChatModelGPT5_2)
	if len(opts) == 0 {
		t.Fatalf("expected context management options for supported model")
	}

	opts = contextManagementOptions("unknown-model")
	if len(opts) != 0 {
		t.Fatalf("expected no context management options for unsupported model")
	}
}

func TestClassifyOpenAIError_InvalidRequestIsNonRetryable(t *testing.T) {
	t.Parallel()

	input := &openai.Error{
		StatusCode: 400,
		Type:       "invalid_request_error",
		Param:      "model",
		Message:    "bad model",
	}

	err := classifyOpenAIError(input)

	var appErr *temporal.ApplicationError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected application error, got %T", err)
	}
	if !appErr.NonRetryable() {
		t.Fatalf("expected non-retryable application error")
	}
	if appErr.Type() != "OpenAIInvalidRequestError" {
		t.Fatalf("expected OpenAIInvalidRequestError type, got %q", appErr.Type())
	}
}

func TestClassifyOpenAIError_URLTimeoutIsRetryable(t *testing.T) {
	t.Parallel()

	input := &openai.Error{
		StatusCode: 400,
		Type:       "invalid_request_error",
		Param:      "url",
		Message:    "Timeout while downloading https://example.com/image.png.",
	}

	err := classifyOpenAIError(input)

	if !errors.Is(err, input) {
		t.Fatalf("expected original retryable error to be returned")
	}

	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) && appErr.NonRetryable() {
		t.Fatalf("expected timeout URL error to remain retryable")
	}
}

func TestClassifyOpenAIError_Any400IsNonRetryable(t *testing.T) {
	t.Parallel()

	input := &openai.Error{
		StatusCode: 400,
		Type:       "bad_request",
		Message:    "bad request",
	}

	err := classifyOpenAIError(input)

	var appErr *temporal.ApplicationError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected application error, got %T", err)
	}
	if !appErr.NonRetryable() {
		t.Fatalf("expected non-retryable application error")
	}
	if appErr.Type() != "OpenAIInvalidRequestError" {
		t.Fatalf("expected OpenAIInvalidRequestError type, got %q", appErr.Type())
	}
}
