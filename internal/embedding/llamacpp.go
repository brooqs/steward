package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

const (
	llamaDefaultPort  = 8787
	llamaDimensions   = 384
	llamaStartTimeout = 30 * time.Second
)

// LlamaCppEmbedder runs llama-server as a subprocess for local embeddings.
// No CGO required — pure subprocess + HTTP API communication.
type LlamaCppEmbedder struct {
	serverBin string // path to llama-server binary
	modelPath string // path to .gguf model file
	port      int
	cmd       *exec.Cmd
	client    *http.Client
	mu        sync.Mutex
	running   bool
}

// NewLlamaCppEmbedder creates and starts a local llama.cpp embedding server.
func NewLlamaCppEmbedder(modelsDir string) (*LlamaCppEmbedder, error) {
	serverBin := filepath.Join(modelsDir, "llama-server")
	modelPath := filepath.Join(modelsDir, "all-MiniLM-L6-v2.Q8_0.gguf")

	// Check files exist
	if _, err := os.Stat(serverBin); err != nil {
		return nil, fmt.Errorf("llama-server binary not found at %s (download via admin panel)", serverBin)
	}
	if _, err := os.Stat(modelPath); err != nil {
		return nil, fmt.Errorf("embedding model not found at %s (download via admin panel)", modelPath)
	}

	e := &LlamaCppEmbedder{
		serverBin: serverBin,
		modelPath: modelPath,
		port:      llamaDefaultPort,
		client:    &http.Client{Timeout: 30 * time.Second},
	}

	if err := e.start(); err != nil {
		return nil, err
	}

	return e, nil
}

func (e *LlamaCppEmbedder) Name() string   { return "llamacpp" }
func (e *LlamaCppEmbedder) Dimensions() int { return llamaDimensions }

func (e *LlamaCppEmbedder) start() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return nil
	}

	modelsDir := filepath.Dir(e.modelPath)
	e.cmd = exec.Command(e.serverBin,
		"--model", e.modelPath,
		"--port", fmt.Sprintf("%d", e.port),
		"--embedding",
		"--ctx-size", "512",
		"--threads", "2",
		"--log-disable",
	)
	// Set LD_LIBRARY_PATH so llama-server can find libllama.so etc.
	e.cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH="+modelsDir)
	e.cmd.Stdout = nil
	e.cmd.Stderr = nil

	if err := e.cmd.Start(); err != nil {
		return fmt.Errorf("starting llama-server: %w", err)
	}

	slog.Info("llama-server starting", "port", e.port, "model", filepath.Base(e.modelPath))

	// Wait for server to be ready
	deadline := time.Now().Add(llamaStartTimeout)
	for time.Now().Before(deadline) {
		resp, err := e.client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", e.port))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				e.running = true
				slog.Info("llama-server ready", "port", e.port)
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Cleanup on failure
	e.cmd.Process.Kill()
	return fmt.Errorf("llama-server failed to start within %s", llamaStartTimeout)
}

// Stop terminates the llama-server subprocess.
func (e *LlamaCppEmbedder) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cmd != nil && e.cmd.Process != nil {
		e.cmd.Process.Kill()
		e.cmd.Wait()
		e.running = false
		slog.Info("llama-server stopped")
	}
}

func (e *LlamaCppEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	vectors, err := e.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 {
		return nil, fmt.Errorf("empty embedding result")
	}
	return vectors[0], nil
}

func (e *LlamaCppEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if !e.running {
		if err := e.start(); err != nil {
			return nil, err
		}
	}

	// llama-server /embedding endpoint accepts {"content": "text"} or {"content": ["text1", "text2"]}
	payload := map[string]any{
		"content": texts,
	}
	body, _ := json.Marshal(payload)

	url := fmt.Sprintf("http://127.0.0.1:%d/embedding", e.port)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llama-server request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("llama-server error %d: %s", resp.StatusCode, string(respBody))
	}

	// Response format: [{"index": 0, "embedding": [[0.1, 0.2, ...]]}]
	// Note: embedding can be nested array [[...]] for sentence-level models
	var rawResponse []byte
	rawResponse, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading embedding response: %w", err)
	}

	// Try nested format first: [{"embedding": [[float, ...]]}]
	var nestedResults []struct {
		Embedding [][]float64 `json:"embedding"`
	}
	if err := json.Unmarshal(rawResponse, &nestedResults); err == nil && len(nestedResults) > 0 && len(nestedResults[0].Embedding) > 0 {
		vectors := make([][]float32, len(nestedResults))
		for i, r := range nestedResults {
			if len(r.Embedding) == 0 {
				continue
			}
			// Use first (and usually only) embedding row
			vec := r.Embedding[0]
			vectors[i] = make([]float32, len(vec))
			for j, v := range vec {
				vectors[i][j] = float32(v)
			}
		}
		return vectors, nil
	}

	// Fallback: flat format [{"embedding": [float, ...]}]
	var flatResults []struct {
		Embedding []float64 `json:"embedding"`
	}
	if err := json.Unmarshal(rawResponse, &flatResults); err != nil {
		return nil, fmt.Errorf("decoding embedding response: %w", err)
	}

	vectors := make([][]float32, len(flatResults))
	for i, r := range flatResults {
		vectors[i] = make([]float32, len(r.Embedding))
		for j, v := range r.Embedding {
			vectors[i][j] = float32(v)
		}
	}

	return vectors, nil
}
