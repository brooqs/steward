package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const geminiBaseURL = "https://generativelanguage.googleapis.com/v1beta"

// Gemini implements the Provider interface for the Google Gemini API.
type Gemini struct {
	apiKey     string
	httpClient *http.Client
}

// NewGemini creates a new Gemini provider.
func NewGemini(apiKey string) *Gemini {
	return &Gemini{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (g *Gemini) Name() string          { return "gemini" }
func (g *Gemini) SupportsToolUse() bool  { return true }

// Gemini API types
type geminiRequest struct {
	Contents         []geminiContent       `json:"contents"`
	SystemInstruction *geminiContent        `json:"systemInstruction,omitempty"`
	Tools            []geminiTool          `json:"tools,omitempty"`
	GenerationConfig *geminiGenerationCfg  `json:"generationConfig,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text             string                `json:"text,omitempty"`
	FunctionCall     *geminiFunctionCall   `json:"functionCall,omitempty"`
	FunctionResponse *geminiFuncResponse   `json:"functionResponse,omitempty"`
}

type geminiFunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type geminiFuncResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type geminiTool struct {
	FunctionDeclarations []geminiFuncDecl `json:"functionDeclarations"`
}

type geminiFuncDecl struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type geminiGenerationCfg struct {
	MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
}

type geminiResponse struct {
	Candidates []geminiCandidate `json:"candidates"`
	Error      *geminiError      `json:"error,omitempty"`
}

type geminiCandidate struct {
	Content      geminiContent `json:"content"`
	FinishReason string        `json:"finishReason"`
}

type geminiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (g *Gemini) ChatCompletion(ctx context.Context, req *Request) (*Response, error) {
	// Build contents
	var contents []geminiContent
	for _, m := range req.Messages {
		gc := geminiContent{}
		switch m.Role {
		case "user":
			gc.Role = "user"
		case "assistant":
			gc.Role = "model"
		default:
			gc.Role = m.Role
		}

		for _, b := range m.Content {
			switch b.Type {
			case "text":
				gc.Parts = append(gc.Parts, geminiPart{Text: b.Text})
			case "tool_use":
				gc.Parts = append(gc.Parts, geminiPart{
					FunctionCall: &geminiFunctionCall{
						Name: b.ToolName,
						Args: b.ToolInput,
					},
				})
			case "tool_result":
				// Tool results go as user messages with function response
				var respData map[string]any
				if err := json.Unmarshal([]byte(b.Content), &respData); err != nil {
					respData = map[string]any{"result": b.Content}
				}
				gc.Role = "user"
				gc.Parts = append(gc.Parts, geminiPart{
					FunctionResponse: &geminiFuncResponse{
						Name:     b.ToolResultID,
						Response: respData,
					},
				})
			}
		}
		if len(gc.Parts) > 0 {
			contents = append(contents, gc)
		}
	}

	// Build tools
	var tools []geminiTool
	if len(req.Tools) > 0 {
		var decls []geminiFuncDecl
		for _, t := range req.Tools {
			decls = append(decls, geminiFuncDecl{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			})
		}
		tools = append(tools, geminiTool{FunctionDeclarations: decls})
	}

	body := geminiRequest{
		Contents: contents,
		Tools:    tools,
	}

	if req.SystemPrompt != "" {
		body.SystemInstruction = &geminiContent{
			Parts: []geminiPart{{Text: req.SystemPrompt}},
		}
	}

	if req.MaxTokens > 0 {
		body.GenerationConfig = &geminiGenerationCfg{
			MaxOutputTokens: req.MaxTokens,
		}
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("gemini: marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", geminiBaseURL, req.Model, g.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gemini: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := g.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gemini: api call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gemini: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gemini: api returned %d: %s", resp.StatusCode, string(respBody))
	}

	var gr geminiResponse
	if err := json.Unmarshal(respBody, &gr); err != nil {
		return nil, fmt.Errorf("gemini: unmarshal response: %w", err)
	}

	if gr.Error != nil {
		return nil, fmt.Errorf("gemini: api error [%d]: %s", gr.Error.Code, gr.Error.Message)
	}

	if len(gr.Candidates) == 0 {
		return nil, fmt.Errorf("gemini: empty response (no candidates)")
	}

	candidate := gr.Candidates[0]
	result := &Response{}

	// Map finish reason
	switch candidate.FinishReason {
	case "STOP":
		result.StopReason = "end_turn"
	case "TOOL_CALLS", "FUNCTION_CALL":
		result.StopReason = "tool_use"
	default:
		result.StopReason = candidate.FinishReason
	}

	// Extract content
	for _, part := range candidate.Content.Parts {
		if part.Text != "" {
			result.Content = append(result.Content, ContentBlock{
				Type: "text",
				Text: part.Text,
			})
		}
		if part.FunctionCall != nil {
			result.Content = append(result.Content, ContentBlock{
				Type:      "tool_use",
				ToolUseID: fmt.Sprintf("call_%s_%d", part.FunctionCall.Name, time.Now().UnixNano()),
				ToolName:  part.FunctionCall.Name,
				ToolInput: part.FunctionCall.Args,
			})
		}
	}

	return result, nil
}
