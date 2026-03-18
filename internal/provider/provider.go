// Package provider defines the LLM provider interface and common types
// for multi-provider support (Claude, OpenAI, Groq, Gemini, Ollama, OpenRouter).
package provider

import "context"

// Provider is the interface that all LLM backends must implement.
type Provider interface {
	// ChatCompletion sends a conversation to the LLM and returns the response.
	ChatCompletion(ctx context.Context, req *Request) (*Response, error)
	// Name returns the provider identifier (e.g., "claude", "openai").
	Name() string
	// SupportsToolUse returns whether this provider supports function/tool calling.
	SupportsToolUse() bool
}

// Request represents a chat completion request.
type Request struct {
	Model        string
	SystemPrompt string
	Messages     []Message
	Tools        []ToolSchema
	MaxTokens    int
}

// Message represents a conversation message.
type Message struct {
	Role    string        `json:"role"`    // user | assistant
	Content []ContentBlock `json:"content"` // can be text or tool results
}

// NewTextMessage is a convenience constructor for a simple text message.
func NewTextMessage(role, text string) Message {
	return Message{
		Role: role,
		Content: []ContentBlock{
			{Type: "text", Text: text},
		},
	}
}

// ContentBlock is a single block within a message.
type ContentBlock struct {
	Type string `json:"type"` // text | tool_use | tool_result

	// For type=text
	Text string `json:"text,omitempty"`

	// For type=tool_use
	ToolUseID string         `json:"id,omitempty"`
	ToolName  string         `json:"name,omitempty"`
	ToolInput map[string]any `json:"input,omitempty"`

	// For type=tool_result
	ToolResultID string `json:"tool_use_id,omitempty"`
	Content      string `json:"content,omitempty"`
}

// ToolSchema describes a tool for the LLM API.
type ToolSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// Response represents a chat completion response.
type Response struct {
	Content    []ContentBlock
	StopReason string // end_turn | tool_use | max_tokens
}

// ExtractText returns all text blocks concatenated.
func (r *Response) ExtractText() string {
	var parts []string
	for _, b := range r.Content {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += "\n" + parts[i]
	}
	return result
}

// ToolCalls returns all tool_use blocks from the response.
func (r *Response) ToolCalls() []ContentBlock {
	var calls []ContentBlock
	for _, b := range r.Content {
		if b.Type == "tool_use" {
			calls = append(calls, b)
		}
	}
	return calls
}
