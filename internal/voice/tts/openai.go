package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OpenAITTS implements TTS using the OpenAI Audio Speech API.
type OpenAITTS struct {
	apiKey     string
	model      string
	httpClient *http.Client
}

// NewOpenAI creates a new OpenAI TTS provider.
func NewOpenAI(apiKey, model string) *OpenAITTS {
	if model == "" {
		model = "tts-1" // tts-1 (fast) or tts-1-hd (quality)
	}
	return &OpenAITTS{
		apiKey: apiKey,
		model:  model,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (o *OpenAITTS) Name() string { return "openai" }

func (o *OpenAITTS) Synthesize(ctx context.Context, text string, opts *Options) ([]byte, error) {
	if opts == nil {
		opts = DefaultOptions()
	}

	voice := opts.Voice
	if voice == "" {
		voice = "alloy" // alloy, echo, fable, onyx, nova, shimmer
	}

	format := string(opts.Format)
	if format == "" {
		format = "mp3"
	}

	body := map[string]any{
		"model":           o.model,
		"input":           text,
		"voice":           voice,
		"response_format": format,
	}
	if opts.Speed > 0 && opts.Speed != 1.0 {
		body["speed"] = opts.Speed
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/audio/speech", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+o.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("api call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("api returned %d: %s", resp.StatusCode, string(errBody))
	}

	audio, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading audio: %w", err)
	}

	return audio, nil
}
