package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	hfInferenceURL = "https://router.huggingface.co/pipeline/feature-extraction/"
	hfDefaultModel = "sentence-transformers/all-MiniLM-L6-v2"
	hfDimensions   = 384
)

// HuggingFaceEmbedder uses the HuggingFace Inference API for embeddings.
// Free for public models, no API key required (rate-limited).
type HuggingFaceEmbedder struct {
	model  string
	apiKey string // optional — removes rate limits
	client *http.Client
}

// NewHuggingFaceEmbedder creates a HuggingFace Inference API embedder.
func NewHuggingFaceEmbedder(apiKey, model string) *HuggingFaceEmbedder {
	if model == "" {
		model = hfDefaultModel
	}
	return &HuggingFaceEmbedder{
		model:  model,
		apiKey: apiKey,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (e *HuggingFaceEmbedder) Name() string       { return "huggingface" }
func (e *HuggingFaceEmbedder) Dimensions() int     { return hfDimensions }

func (e *HuggingFaceEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	vectors, err := e.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return vectors[0], nil
}

func (e *HuggingFaceEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	payload := map[string]any{
		"inputs": texts,
		"options": map[string]any{
			"wait_for_model": true,
		},
	}
	body, _ := json.Marshal(payload)

	url := hfInferenceURL + e.model
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HF API request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HF API error %d: %s", resp.StatusCode, string(respBody))
	}

	// Response is [][]float64 for batch
	var rawResult [][]float64
	if err := json.NewDecoder(resp.Body).Decode(&rawResult); err != nil {
		return nil, fmt.Errorf("decoding HF response: %w", err)
	}

	// Convert float64 → float32
	result := make([][]float32, len(rawResult))
	for i, vec := range rawResult {
		result[i] = make([]float32, len(vec))
		for j, v := range vec {
			result[i][j] = float32(v)
		}
	}

	return result, nil
}
