package llm

import "strings"

const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

const (
	ContentTypeText             = "text"
	ContentTypeImageURL         = "image_url"
	ContentTypeThinking         = "thinking"
	ContentTypeRedactedThinking = "redacted_thinking"
)

type ResponseTextFormat struct {
	Name   string `json:"name"`
	Schema any    `json:"schema"`
	Strict bool   `json:"strict"`
}

type ContentPart struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	ImageURL    string `json:"image_url,omitempty"`
	ImageDetail string `json:"image_detail,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	Signature   string `json:"signature,omitempty"`
	Data        string `json:"data,omitempty"`
}

type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	Strict      bool           `json:"strict,omitempty"`
}

type ToolCall struct {
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Message struct {
	Role       string        `json:"role"`
	Content    []ContentPart `json:"content,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
	ToolName   string        `json:"tool_name,omitempty"`
	ToolCalls  []ToolCall    `json:"tool_calls,omitempty"`
}

func TextPart(text string) ContentPart {
	return ContentPart{Type: ContentTypeText, Text: text}
}

func ImageURLPart(url string, detail ...string) ContentPart {
	imageDetail := ""
	if len(detail) > 0 {
		imageDetail = detail[0]
	}
	return ContentPart{Type: ContentTypeImageURL, ImageURL: url, ImageDetail: imageDetail}
}

func ThinkingPart(signature, thinking string) ContentPart {
	return ContentPart{
		Type:      ContentTypeThinking,
		Thinking:  thinking,
		Signature: signature,
	}
}

func RedactedThinkingPart(data string) ContentPart {
	return ContentPart{
		Type: ContentTypeRedactedThinking,
		Data: data,
	}
}

func TextMessage(role string, text string) Message {
	return Message{
		Role:    role,
		Content: []ContentPart{TextPart(text)},
	}
}

func ToolResultMessage(callID, name, output string) Message {
	return Message{
		Role:       RoleTool,
		ToolCallID: callID,
		ToolName:   name,
		Content:    []ContentPart{TextPart(output)},
	}
}

func (m Message) Text() string {
	parts := make([]string, 0, len(m.Content))
	for _, part := range m.Content {
		if part.Type == ContentTypeText {
			parts = append(parts, part.Text)
		}
	}
	return strings.Join(parts, "")
}

type ConversationState struct {
	Backend              string    `json:"backend"`
	Provider             string    `json:"provider"`
	OpenAIConversationID string    `json:"openai_conversation_id,omitempty"`
	Transcript           []Message `json:"transcript,omitempty"`
}

type ConversationRequest struct {
	Model string    `json:"model"`
	Items []Message `json:"items,omitempty"`
}

type Request struct {
	Model        string              `json:"model"`
	Messages     []Message           `json:"messages"`
	Tools        []ToolDefinition    `json:"tools,omitempty"`
	Temperature  *float64            `json:"temperature,omitempty"`
	Text         *ResponseTextFormat `json:"text,omitempty"`
	Instructions string              `json:"instructions,omitempty"`
	Conversation *ConversationState  `json:"conversation,omitempty"`
}

type Response struct {
	OutputText     string             `json:"output_text"`
	ToolCalls      []ToolCall         `json:"tool_calls,omitempty"`
	Conversation   *ConversationState `json:"conversation,omitempty"`
	StopReason     string             `json:"stop_reason,omitempty"`
	ShouldContinue bool               `json:"should_continue,omitempty"`
}

func (s ConversationState) Clone() ConversationState {
	clone := s
	if len(s.Transcript) == 0 {
		return clone
	}
	clone.Transcript = make([]Message, len(s.Transcript))
	for i, msg := range s.Transcript {
		clone.Transcript[i] = cloneMessage(msg)
	}
	return clone
}

func cloneMessage(msg Message) Message {
	clone := msg
	if len(msg.Content) > 0 {
		clone.Content = append([]ContentPart(nil), msg.Content...)
	}
	if len(msg.ToolCalls) > 0 {
		clone.ToolCalls = append([]ToolCall(nil), msg.ToolCalls...)
	}
	return clone
}
