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

// Well-known base URLs for OpenAI-compatible providers.
var openAIBaseURLs = map[string]string{
	"openai":     "https://api.openai.com/v1",
	"groq":       "https://api.groq.com/openai/v1",
	"openrouter": "https://openrouter.ai/api/v1",
	"ollama":     "http://localhost:11434/v1",
}

// OpenAI implements the Provider interface for OpenAI-compatible APIs.
// This covers OpenAI, Groq, OpenRouter, and Ollama.
type OpenAI struct {
	name       string
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewOpenAI creates a new OpenAI-compatible provider.
// providerName should be one of: openai, groq, openrouter, ollama.
// If customBaseURL is set, it overrides the default for the provider.
func NewOpenAI(providerName, apiKey, customBaseURL string) *OpenAI {
	baseURL := customBaseURL
	if baseURL == "" {
		if url, ok := openAIBaseURLs[providerName]; ok {
			baseURL = url
		} else {
			baseURL = openAIBaseURLs["openai"]
		}
	}

	return &OpenAI{
		name:   providerName,
		apiKey: apiKey,
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (o *OpenAI) Name() string         { return o.name }
func (o *OpenAI) SupportsToolUse() bool { return true }

// OpenAI API types
type oaiRequest struct {
	Model       string        `json:"model"`
	Messages    []oaiMessage  `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Tools       []oaiTool     `json:"tools,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
}

type oaiMessage struct {
	Role       string         `json:"role"`
	Content    any            `json:"content"`           // string or null
	ToolCalls  []oaiToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type oaiTool struct {
	Type     string      `json:"type"`
	Function oaiFunction `json:"function"`
}

type oaiFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type oaiToolCall struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Function oaiToolCallFunc `json:"function"`
}

type oaiToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

type oaiResponse struct {
	ID      string      `json:"id"`
	Choices []oaiChoice `json:"choices"`
	Error   *oaiError   `json:"error,omitempty"`
}

type oaiChoice struct {
	Message      oaiMessage `json:"message"`
	FinishReason string     `json:"finish_reason"`
}

type oaiError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

func (o *OpenAI) ChatCompletion(ctx context.Context, req *Request) (*Response, error) {
	apiStart := time.Now()
	slog.Debug("llm request", "provider", o.name, "model", req.Model, "messages", len(req.Messages), "tools", len(req.Tools))

	// Build messages — system prompt goes as a system message
	msgs := make([]oaiMessage, 0, len(req.Messages)+1)
	if req.SystemPrompt != "" {
		msgs = append(msgs, oaiMessage{Role: "system", Content: req.SystemPrompt})
	}

	for _, m := range req.Messages {
		om := oaiMessage{Role: m.Role}

		// Check if this is a tool_result message
		if len(m.Content) > 0 && m.Content[0].Type == "tool_result" {
			om.Role = "tool"
			om.ToolCallID = m.Content[0].ToolResultID
			om.Content = m.Content[0].Content
			msgs = append(msgs, om)
			continue
		}

		// Check if this is an assistant message with tool calls
		hasToolCalls := false
		for _, b := range m.Content {
			if b.Type == "tool_use" {
				hasToolCalls = true
				break
			}
		}

		if hasToolCalls {
			om.Role = "assistant"
			var textParts string
			for _, b := range m.Content {
				switch b.Type {
				case "text":
					textParts += b.Text
				case "tool_use":
					argsJSON, _ := json.Marshal(b.ToolInput)
					om.ToolCalls = append(om.ToolCalls, oaiToolCall{
						ID:   b.ToolUseID,
						Type: "function",
						Function: oaiToolCallFunc{
							Name:      b.ToolName,
							Arguments: string(argsJSON),
						},
					})
				}
			}
			if textParts != "" {
				om.Content = textParts
			}
			msgs = append(msgs, om)

			// For OpenAI format, each tool result must be a separate message
			for _, b := range m.Content {
				if b.Type == "tool_result" {
					msgs = append(msgs, oaiMessage{
						Role:       "tool",
						ToolCallID: b.ToolResultID,
						Content:    b.Content,
					})
				}
			}
			continue
		}

		// Simple text message
		if len(m.Content) == 1 && m.Content[0].Type == "text" {
			om.Content = m.Content[0].Text
		} else {
			var text string
			for _, b := range m.Content {
				if b.Type == "text" {
					text += b.Text
				}
			}
			om.Content = text
		}
		msgs = append(msgs, om)
	}

	// Build tools
	var tools []oaiTool
	for _, t := range req.Tools {
		tools = append(tools, oaiTool{
			Type: "function",
			Function: oaiFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}

	body := oaiRequest{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		Messages:  msgs,
		Tools:     tools,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("%s: marshal request: %w", o.name, err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", o.baseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s: create request: %w", o.name, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if o.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)
	}

	resp, err := o.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%s: api call: %w", o.name, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%s: read response: %w", o.name, err)
	}

	if resp.StatusCode != http.StatusOK {
		slog.Error("llm api error", "provider", o.name, "status", resp.StatusCode, "duration", time.Since(apiStart), "body", string(respBody))
		return nil, fmt.Errorf("%s: api returned %d: %s", o.name, resp.StatusCode, string(respBody))
	}

	var oResp oaiResponse
	if err := json.Unmarshal(respBody, &oResp); err != nil {
		return nil, fmt.Errorf("%s: unmarshal response: %w", o.name, err)
	}

	if oResp.Error != nil {
		return nil, fmt.Errorf("%s: api error [%s]: %s", o.name, oResp.Error.Type, oResp.Error.Message)
	}

	if len(oResp.Choices) == 0 {
		return nil, fmt.Errorf("%s: empty response (no choices)", o.name)
	}

	choice := oResp.Choices[0]
	result := &Response{}

	// Map finish_reason to our format
	switch choice.FinishReason {
	case "stop":
		result.StopReason = "end_turn"
	case "tool_calls":
		result.StopReason = "tool_use"
	default:
		result.StopReason = choice.FinishReason
	}

	slog.Info("llm response",
		"provider", o.name,
		"duration", time.Since(apiStart),
		"stop_reason", result.StopReason,
		"tool_calls", len(choice.Message.ToolCalls),
	)

	// Extract text content
	if text, ok := choice.Message.Content.(string); ok && text != "" {
		result.Content = append(result.Content, ContentBlock{
			Type: "text",
			Text: text,
		})
	}

	// Extract tool calls
	for _, tc := range choice.Message.ToolCalls {
		var input map[string]any
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
			input = map[string]any{"_raw": tc.Function.Arguments}
		}
		result.Content = append(result.Content, ContentBlock{
			Type:      "tool_use",
			ToolUseID: tc.ID,
			ToolName:  tc.Function.Name,
			ToolInput: input,
		})
	}

	return result, nil
}
