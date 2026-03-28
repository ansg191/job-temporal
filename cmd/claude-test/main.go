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
//	go run ./cmd/claude-test -tools                       # test tool calling
//	CLAUDE_AUTH_FILE=/path/to/auth.json go run ./cmd/claude-test
package main

import (
	"context"
	"encoding/json"
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
	tools := flag.Bool("tools", false, "Test tool calling with a get_weather tool")
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

	if *tools {
		runToolTest(backend, ref.Model)
		return
	}

	runBasicTest(backend, ref.Model, *prompt)
}

func runBasicTest(backend llm.Backend, model, prompt string) {
	fmt.Fprintf(os.Stderr, "--- claude-test ---\n")
	fmt.Fprintf(os.Stderr, "model:  %s\n", model)
	fmt.Fprintf(os.Stderr, "prompt: %s\n", prompt)
	fmt.Fprintf(os.Stderr, "---\n")

	resp, err := backend.Generate(context.Background(), llm.Request{
		Model: model,
		Messages: []llm.Message{
			llm.TextMessage(llm.RoleUser, prompt),
		},
	})
	if err != nil {
		log.Fatalf("Generate failed: %v", err)
	}

	printResponseMeta(resp)
	fmt.Println(strings.TrimSpace(resp.OutputText))
}

func runToolTest(backend llm.Backend, model string) {
	fmt.Fprintf(os.Stderr, "--- claude-test (tool calling) ---\n")
	fmt.Fprintf(os.Stderr, "model: %s\n", model)
	fmt.Fprintf(os.Stderr, "---\n")

	weatherTool := llm.ToolDefinition{
		Name:        "get_weather",
		Description: "Get the current weather for a city.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"city": map[string]any{
					"type":        "string",
					"description": "City name, e.g. San Francisco",
				},
			},
			"required": []string{"city"},
		},
	}

	prompt := "What's the weather in San Francisco? Use the get_weather tool."

	fmt.Fprintf(os.Stderr, "prompt: %s\n", prompt)
	fmt.Fprintf(os.Stderr, "tool:   %s\n", weatherTool.Name)
	fmt.Fprintf(os.Stderr, "---\n")

	// Turn 1: model should call the tool
	fmt.Fprintf(os.Stderr, "[turn 1] sending prompt with tool...\n")
	resp, err := backend.Generate(context.Background(), llm.Request{
		Model: model,
		Messages: []llm.Message{
			llm.TextMessage(llm.RoleUser, prompt),
		},
		Tools: []llm.ToolDefinition{weatherTool},
	})
	if err != nil {
		log.Fatalf("Turn 1 Generate failed: %v", err)
	}

	printResponseMeta(resp)

	if len(resp.ToolCalls) == 0 {
		log.Fatalf("FAIL: expected tool call, got none")
	}

	call := resp.ToolCalls[0]
	fmt.Fprintf(os.Stderr, "[turn 1] tool_call: %s(%s)\n", call.Name, call.Arguments)

	if call.Name != "get_weather" {
		log.Fatalf("FAIL: expected get_weather call, got %q", call.Name)
	}

	var args map[string]string
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
		log.Fatalf("FAIL: could not parse tool args: %v", err)
	}
	fmt.Fprintf(os.Stderr, "[turn 1] parsed city: %s\n", args["city"])
	fmt.Fprintf(os.Stderr, "---\n")

	// Turn 2: return fake weather result, model should summarize
	fakeResult := `{"city": "San Francisco", "temp_f": 62, "condition": "foggy", "humidity": 78}`
	fmt.Fprintf(os.Stderr, "[turn 2] returning tool result: %s\n", fakeResult)

	resp2, err := backend.Generate(context.Background(), llm.Request{
		Model: model,
		Messages: []llm.Message{
			llm.ToolResultMessage(call.CallID, call.Name, fakeResult),
		},
		Tools:        []llm.ToolDefinition{weatherTool},
		Conversation: resp.Conversation,
	})
	if err != nil {
		log.Fatalf("Turn 2 Generate failed: %v", err)
	}

	printResponseMeta(resp2)

	if resp2.OutputText == "" {
		log.Fatalf("FAIL: expected text response after tool result, got empty")
	}

	fmt.Fprintf(os.Stderr, "---\n")
	fmt.Fprintf(os.Stderr, "PASS: tool calling round-trip successful\n")
	fmt.Fprintf(os.Stderr, "---\n")
	fmt.Println(strings.TrimSpace(resp2.OutputText))
}

func printResponseMeta(resp *llm.Response) {
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
}
