package provider

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

const (
	llamaChatPort         = 8788
	llamaChatStartTimeout = 60 * time.Second
)

// LlamaCpp implements the Provider interface using a local llama-server subprocess.
// This is used for FunctionGemma and other local GGUF models for tool calling.
type LlamaCpp struct {
	oai     *OpenAI // reuse OpenAI provider for API communication
	cmd     *exec.Cmd
	mu      sync.Mutex
	running bool

	serverBin string
	modelPath string
	modelsDir string
}

// NewLlamaCpp creates a new local llama.cpp provider.
// modelsDir should contain llama-server binary and the GGUF model.
func NewLlamaCpp(modelsDir, modelFile string) (*LlamaCpp, error) {
	serverBin := filepath.Join(modelsDir, "llama-server")
	modelPath := filepath.Join(modelsDir, modelFile)

	if _, err := os.Stat(serverBin); err != nil {
		return nil, fmt.Errorf("llama-server binary not found at %s", serverBin)
	}
	if _, err := os.Stat(modelPath); err != nil {
		return nil, fmt.Errorf("model not found at %s", modelPath)
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d/v1", llamaChatPort)

	lc := &LlamaCpp{
		oai:       NewOpenAI("llamacpp", "", baseURL),
		serverBin: serverBin,
		modelPath: modelPath,
		modelsDir: modelsDir,
	}

	if err := lc.start(); err != nil {
		return nil, err
	}

	return lc, nil
}

func (lc *LlamaCpp) Name() string         { return "llamacpp" }
func (lc *LlamaCpp) SupportsToolUse() bool { return true }

func (lc *LlamaCpp) start() error {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	if lc.running {
		return nil
	}

	lc.cmd = exec.Command(lc.serverBin,
		"--model", lc.modelPath,
		"--port", fmt.Sprintf("%d", llamaChatPort),
		"--host", "127.0.0.1",
		"--jinja",
		"--ctx-size", "4096",
		"--threads", "4",
		"--log-disable",
	)
	lc.cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH="+lc.modelsDir)
	lc.cmd.Stdout = nil
	lc.cmd.Stderr = nil

	if err := lc.cmd.Start(); err != nil {
		return fmt.Errorf("starting llama-server for chat: %w", err)
	}

	slog.Info("llama-server (chat) starting", "port", llamaChatPort, "model", filepath.Base(lc.modelPath))

	// Wait for server to be ready
	client := lc.oai.httpClient
	deadline := time.Now().Add(llamaChatStartTimeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", llamaChatPort))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				lc.running = true
				slog.Info("llama-server (chat) ready", "port", llamaChatPort)
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	lc.cmd.Process.Kill()
	return fmt.Errorf("llama-server (chat) failed to start within %s", llamaChatStartTimeout)
}

// Stop terminates the llama-server subprocess.
func (lc *LlamaCpp) Stop() {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	if lc.cmd != nil && lc.cmd.Process != nil {
		lc.cmd.Process.Kill()
		lc.cmd.Wait()
		lc.running = false
		slog.Info("llama-server (chat) stopped")
	}
}

// ChatCompletion delegates to the OpenAI-compatible implementation.
func (lc *LlamaCpp) ChatCompletion(ctx context.Context, req *Request) (*Response, error) {
	return lc.oai.ChatCompletion(ctx, req)
}
