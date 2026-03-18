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

const elevenLabsBaseURL = "https://api.elevenlabs.io/v1"

// ElevenLabs implements TTS using the ElevenLabs API.
// Known for high-quality, natural-sounding voices.
type ElevenLabs struct {
	apiKey     string
	modelID    string
	httpClient *http.Client
}

// NewElevenLabs creates a new ElevenLabs TTS provider.
func NewElevenLabs(apiKey, modelID string) *ElevenLabs {
	if modelID == "" {
		modelID = "eleven_multilingual_v2"
	}
	return &ElevenLabs{
		apiKey:  apiKey,
		modelID: modelID,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (e *ElevenLabs) Name() string { return "elevenlabs" }

func (e *ElevenLabs) Synthesize(ctx context.Context, text string, opts *Options) ([]byte, error) {
	if opts == nil {
		opts = DefaultOptions()
	}

	voiceID := opts.Voice
	if voiceID == "" {
		voiceID = "21m00Tcm4TlvDq8ikWAM" // Rachel — default ElevenLabs voice
	}

	body := map[string]any{
		"text":     text,
		"model_id": e.modelID,
		"voice_settings": map[string]any{
			"stability":        0.5,
			"similarity_boost": 0.75,
			"style":            0.0,
		},
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/text-to-speech/%s", elevenLabsBaseURL, voiceID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("xi-api-key", e.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "audio/mpeg")

	format := string(opts.Format)
	if format != "" && format != "mp3" {
		// ElevenLabs supports: mp3_44100_128, pcm_16000, ulaw_8000
		switch AudioFormat(format) {
		case FormatWAV:
			req.Header.Set("Accept", "audio/wav")
		case FormatOGG:
			req.Header.Set("Accept", "audio/ogg")
		}
	}

	resp, err := e.httpClient.Do(req)
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
