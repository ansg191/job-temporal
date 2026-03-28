// claude-test is a manual smoke-test for the claude backend.
//
// It reads OAuth credentials from auth.json (see CLAUDE_AUTH_FILE env var)
// and sends a simple message to the Anthropic API using the claude backend's
// Bearer-token auth flow. This validates that the auth.json parsing, header
// injection, and API round-trip all work end-to-end.
//
// Usage:
//
//	go run ./cmd/claude-test                              # defaults to claude-haiku-4-5
//	go run ./cmd/claude-test -model claude-sonnet-4-6     # specific model
//	go run ./cmd/claude-test -prompt "Explain Go channels in one sentence"
//	CLAUDE_AUTH_FILE=/path/to/auth.json go run ./cmd/claude-test
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/ansg191/job-temporal/internal/llm"
)

func main() {
	model := flag.String("model", "claude-haiku-4-5", "Claude model ID (without claude/ prefix)")
	prompt := flag.String("prompt", "Say hello in exactly 5 words.", "Prompt to send")
	flag.Parse()

	modelRef := "claude/" + *model
	ref, err := llm.ParseModelRef(modelRef)
	if err != nil {
		log.Fatalf("invalid model ref %q: %v", modelRef, err)
	}

	backend, err := llm.NewBackend(ref)
	if err != nil {
		log.Fatalf("failed to create backend: %v", err)
	}

	fmt.Fprintf(os.Stderr, "--- claude-test ---\n")
	fmt.Fprintf(os.Stderr, "model:  %s\n", ref.Model)
	fmt.Fprintf(os.Stderr, "prompt: %s\n", *prompt)
	fmt.Fprintf(os.Stderr, "---\n")

	resp, err := backend.Generate(context.Background(), llm.Request{
		Model: ref.Model,
		Messages: []llm.Message{
			llm.TextMessage(llm.RoleUser, *prompt),
		},
	})
	if err != nil {
		log.Fatalf("Generate failed: %v", err)
	}

	fmt.Fprintf(os.Stderr, "stop_reason: %s\n", resp.StopReason)
	fmt.Fprintf(os.Stderr, "tool_calls:  %d\n", len(resp.ToolCalls))

	if resp.Conversation != nil {
		for _, msg := range resp.Conversation.Transcript {
			for _, part := range msg.Content {
				switch part.Type {
				case "thinking":
					fmt.Fprintf(os.Stderr, "thinking:    %d chars\n", len(part.Thinking))
				case "redacted_thinking":
					fmt.Fprintf(os.Stderr, "thinking:    [redacted]\n")
				}
			}
		}
	}

	fmt.Fprintf(os.Stderr, "---\n")
	fmt.Println(strings.TrimSpace(resp.OutputText))
}
