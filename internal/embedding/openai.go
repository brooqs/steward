package embedding

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAIEmbedder generates embeddings using the OpenAI Embeddings API.
// Also works with Ollama (which is OpenAI-compatible).
type OpenAIEmbedder struct {
	apiKey  string
	baseURL string
	model   string
	dims    int
	client  *http.Client
}

// NewOpenAIEmbedder creates an OpenAI or compatible embedding provider.
func NewOpenAIEmbedder(apiKey, baseURL, model string) *OpenAIEmbedder {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "text-embedding-3-small"
	}
	dims := 1536
	switch {
	case strings.Contains(model, "3-small"):
		dims = 1536
	case strings.Contains(model, "3-large"):
		dims = 3072
	case strings.Contains(model, "ada"):
		dims = 1536
	}
	return &OpenAIEmbedder{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		dims:    dims,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (o *OpenAIEmbedder) Name() string       { return "openai" }
func (o *OpenAIEmbedder) Dimensions() int     { return o.dims }

func (o *OpenAIEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	vecs, err := o.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}

func (o *OpenAIEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	body := map[string]any{
		"input": texts,
		"model": o.model,
	}
	data, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, "POST", o.baseURL+"/embeddings",
		strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if o.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding api call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding api returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	vecs := make([][]float32, len(texts))
	for _, d := range result.Data {
		if d.Index < len(vecs) {
			vecs[d.Index] = d.Embedding
			if o.dims == 0 || o.dims != len(d.Embedding) {
				o.dims = len(d.Embedding)
			}
		}
	}

	return vecs, nil
}
