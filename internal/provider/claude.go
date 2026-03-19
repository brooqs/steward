package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

const claudeAPIURL = "https://api.anthropic.com/v1/messages"
const claudeAPIVersion = "2023-06-01"

// Claude implements the Provider interface for the Anthropic Claude API.
type Claude struct {
	apiKey     string
	httpClient *http.Client
}

// NewClaude creates a new Claude provider.
func NewClaude(apiKey string) *Claude {
	return &Claude{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (c *Claude) Name() string          { return "claude" }
func (c *Claude) SupportsToolUse() bool  { return true }

// claudeRequest is the Anthropic Messages API request body.
type claudeRequest struct {
	Model     string           `json:"model"`
	MaxTokens int              `json:"max_tokens"`
	System    string           `json:"system,omitempty"`
	Messages  []claudeMessage  `json:"messages"`
	Tools     []claudeTool     `json:"tools,omitempty"`
}

type claudeMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string or []claudeContentBlock
}

type claudeContentBlock struct {
	Type      string         `json:"-"`
	Text      string         `json:"-"`
	ID        string         `json:"-"`
	Name      string         `json:"-"`
	Input     map[string]any `json:"-"`
	ToolUseID string         `json:"-"`
	Content   string         `json:"-"`
}

// MarshalJSON serializes each content block type with exactly the fields
// Claude expects — no extra fields that trigger "Extra inputs not permitted".
func (b claudeContentBlock) MarshalJSON() ([]byte, error) {
	switch b.Type {
	case "tool_use":
		input := b.Input
		if input == nil {
			input = map[string]any{}
		}
		return json.Marshal(struct {
			Type  string         `json:"type"`
			ID    string         `json:"id"`
			Name  string         `json:"name"`
			Input map[string]any `json:"input"`
		}{b.Type, b.ID, b.Name, input})
	case "tool_result":
		return json.Marshal(struct {
			Type      string `json:"type"`
			ToolUseID string `json:"tool_use_id"`
			Content   string `json:"content"`
		}{b.Type, b.ToolUseID, b.Content})
	default: // text
		return json.Marshal(struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{b.Type, b.Text})
	}
}

// UnmarshalJSON parses Claude API response content blocks.
func (b *claudeContentBlock) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type      string         `json:"type"`
		Text      string         `json:"text"`
		ID        string         `json:"id"`
		Name      string         `json:"name"`
		Input     map[string]any `json:"input"`
		ToolUseID string         `json:"tool_use_id"`
		Content   string         `json:"content"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	b.Type = raw.Type
	b.Text = raw.Text
	b.ID = raw.ID
	b.Name = raw.Name
	b.Input = raw.Input
	b.ToolUseID = raw.ToolUseID
	b.Content = raw.Content
	return nil
}

type claudeTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// claudeResponse is the Anthropic Messages API response body.
type claudeResponse struct {
	ID         string               `json:"id"`
	Type       string               `json:"type"`
	Role       string               `json:"role"`
	Content    []claudeContentBlock `json:"content"`
	StopReason string               `json:"stop_reason"`
	Error      *claudeError         `json:"error,omitempty"`
}

type claudeError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func (c *Claude) ChatCompletion(ctx context.Context, req *Request) (*Response, error) {
	apiStart := time.Now()
	slog.Debug("llm request", "provider", "claude", "model", req.Model, "messages", len(req.Messages), "tools", len(req.Tools))

	// Convert messages
	msgs := make([]claudeMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		cm := claudeMessage{Role: m.Role}
		if len(m.Content) == 1 && m.Content[0].Type == "text" {
			cm.Content = m.Content[0].Text
		} else {
			blocks := make([]claudeContentBlock, 0, len(m.Content))
			for _, b := range m.Content {
				cb := claudeContentBlock{Type: b.Type}
				switch b.Type {
				case "text":
					cb.Text = b.Text
				case "tool_use":
					cb.ID = b.ToolUseID
					cb.Name = b.ToolName
					cb.Input = b.ToolInput
					if cb.Input == nil {
						cb.Input = map[string]any{}
					}
				case "tool_result":
					cb.ToolUseID = b.ToolResultID
					cb.Content = b.Content
				}
				blocks = append(blocks, cb)
			}
			cm.Content = blocks
		}
		msgs = append(msgs, cm)
	}

	// Convert tools
	var tools []claudeTool
	for _, t := range req.Tools {
		tools = append(tools, claudeTool(t))
	}

	body := claudeRequest{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		System:    req.SystemPrompt,
		Messages:  msgs,
		Tools:     tools,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("claude: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", claudeAPIURL, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("claude: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", claudeAPIVersion)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("claude: api call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("claude: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		slog.Error("llm api error", "provider", "claude", "status", resp.StatusCode, "duration", time.Since(apiStart), "body", string(respBody))
		return nil, fmt.Errorf("claude: api returned %d: %s", resp.StatusCode, string(respBody))
	}

	var cr claudeResponse
	if err := json.Unmarshal(respBody, &cr); err != nil {
		return nil, fmt.Errorf("claude: unmarshal response: %w", err)
	}

	if cr.Error != nil {
		return nil, fmt.Errorf("claude: api error [%s]: %s", cr.Error.Type, cr.Error.Message)
	}

	// Convert response
	result := &Response{
		StopReason: cr.StopReason,
	}
	for _, b := range cr.Content {
		cb := ContentBlock{Type: b.Type}
		switch b.Type {
		case "text":
			cb.Text = b.Text
		case "tool_use":
			cb.ToolUseID = b.ID
			cb.ToolName = b.Name
			cb.ToolInput = b.Input
		}
		result.Content = append(result.Content, cb)
	}

	toolCallCount := 0
	for _, b := range cr.Content {
		if b.Type == "tool_use" {
			toolCallCount++
		}
	}
	slog.Info("llm response",
		"provider", "claude",
		"duration", time.Since(apiStart),
		"stop_reason", cr.StopReason,
		"tool_calls", toolCallCount,
	)

	return result, nil
}
