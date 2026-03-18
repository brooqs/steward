package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

// Groq implements STT using the Groq Whisper API.
// Groq provides very fast Whisper inference (~10x realtime).
type Groq struct {
	apiKey     string
	model      string
	httpClient *http.Client
}

// NewGroq creates a new Groq Whisper STT provider.
func NewGroq(apiKey, model string) *Groq {
	if model == "" {
		model = "whisper-large-v3-turbo"
	}
	return &Groq{
		apiKey: apiKey,
		model:  model,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (g *Groq) Name() string { return "groq" }

func (g *Groq) Transcribe(ctx context.Context, audioData []byte, format string) (*Result, error) {
	return whisperAPITranscribe(ctx, g.httpClient, "https://api.groq.com/openai/v1/audio/transcriptions", g.apiKey, g.model, audioData, format)
}

// OpenAISTT implements STT using the OpenAI Whisper API.
type OpenAISTT struct {
	apiKey     string
	model      string
	httpClient *http.Client
}

// NewOpenAI creates a new OpenAI Whisper STT provider.
func NewOpenAI(apiKey, model string) *OpenAISTT {
	if model == "" {
		model = "whisper-1"
	}
	return &OpenAISTT{
		apiKey: apiKey,
		model:  model,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (o *OpenAISTT) Name() string { return "openai" }

func (o *OpenAISTT) Transcribe(ctx context.Context, audioData []byte, format string) (*Result, error) {
	return whisperAPITranscribe(ctx, o.httpClient, "https://api.openai.com/v1/audio/transcriptions", o.apiKey, o.model, audioData, format)
}

// whisperAPITranscribe is the shared implementation for OpenAI-compatible
// Whisper APIs (OpenAI, Groq).
func whisperAPITranscribe(
	ctx context.Context,
	client *http.Client,
	endpoint, apiKey, model string,
	audioData []byte,
	format string,
) (*Result, error) {
	if format == "" {
		format = "wav"
	}

	// Build multipart form
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// File field
	filename := "audio." + format
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, fmt.Errorf("creating form file: %w", err)
	}
	if _, err := part.Write(audioData); err != nil {
		return nil, fmt.Errorf("writing audio data: %w", err)
	}

	// Model field
	if err := writer.WriteField("model", model); err != nil {
		return nil, fmt.Errorf("writing model field: %w", err)
	}

	// Response format
	if err := writer.WriteField("response_format", "verbose_json"); err != nil {
		return nil, fmt.Errorf("writing format field: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("closing multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, &buf)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("api call: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api returned %d: %s", resp.StatusCode, string(body))
	}

	var whisperResp struct {
		Text     string  `json:"text"`
		Language string  `json:"language"`
		Duration float64 `json:"duration"`
	}
	if err := json.Unmarshal(body, &whisperResp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	return &Result{
		Text:     whisperResp.Text,
		Language: whisperResp.Language,
		Duration: whisperResp.Duration,
	}, nil
}
