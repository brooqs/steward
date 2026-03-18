// Package voice provides the voice engine that coordinates STT, TTS,
// and optional audio I/O (VAD, microphone, speaker).
package voice

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/brooqs/steward/internal/voice/stt"
	"github.com/brooqs/steward/internal/voice/tts"
)

// Config holds voice system configuration.
type Config struct {
	// STT settings
	STT STTConfig `yaml:"stt"`
	// TTS settings
	TTS TTSConfig `yaml:"tts"`
}

// STTConfig configures the speech-to-text provider.
type STTConfig struct {
	Provider   string `yaml:"provider"`    // groq | openai | local
	APIKey     string `yaml:"api_key"`
	Model      string `yaml:"model"`
	// Local whisper.cpp settings
	BinaryPath string `yaml:"binary_path"` // path to whisper-cpp binary
	ModelPath  string `yaml:"model_path"`  // path to GGML model
	Language   string `yaml:"language"`    // language hint
	Threads    int    `yaml:"threads"`
}

// TTSConfig configures the text-to-speech provider.
type TTSConfig struct {
	Provider   string `yaml:"provider"`    // openai | elevenlabs | piper
	APIKey     string `yaml:"api_key"`
	Model      string `yaml:"model"`
	Voice      string `yaml:"voice"`       // voice ID
	// Local piper settings
	BinaryPath string `yaml:"binary_path"` // path to piper binary
	ModelPath  string `yaml:"model_path"`  // path to .onnx model
}

// Engine manages the voice pipeline: STT → process → TTS.
type Engine struct {
	stt stt.STT
	tts tts.TTS
}

// NewEngine creates a voice engine with the configured providers.
func NewEngine(cfg Config) (*Engine, error) {
	e := &Engine{}

	// Initialize STT
	if cfg.STT.Provider != "" {
		s, err := createSTT(cfg.STT)
		if err != nil {
			return nil, fmt.Errorf("creating STT: %w", err)
		}
		e.stt = s
		slog.Info("voice STT ready", "provider", s.Name())
	}

	// Initialize TTS
	if cfg.TTS.Provider != "" {
		t, err := createTTS(cfg.TTS)
		if err != nil {
			return nil, fmt.Errorf("creating TTS: %w", err)
		}
		e.tts = t
		slog.Info("voice TTS ready", "provider", t.Name())
	}

	return e, nil
}

// Transcribe converts audio to text using the configured STT provider.
func (e *Engine) Transcribe(ctx context.Context, audio []byte, format string) (string, error) {
	if e.stt == nil {
		return "", fmt.Errorf("STT not configured")
	}
	result, err := e.stt.Transcribe(ctx, audio, format)
	if err != nil {
		return "", err
	}
	return result.Text, nil
}

// Speak converts text to audio using the configured TTS provider.
func (e *Engine) Speak(ctx context.Context, text string, opts *tts.Options) ([]byte, error) {
	if e.tts == nil {
		return nil, fmt.Errorf("TTS not configured")
	}
	return e.tts.Synthesize(ctx, text, opts)
}

// HasSTT returns whether STT is available.
func (e *Engine) HasSTT() bool { return e.stt != nil }

// HasTTS returns whether TTS is available.
func (e *Engine) HasTTS() bool { return e.tts != nil }

func createSTT(cfg STTConfig) (stt.STT, error) {
	switch strings.ToLower(cfg.Provider) {
	case "groq":
		if cfg.APIKey == "" {
			return nil, fmt.Errorf("groq STT requires api_key")
		}
		return stt.NewGroq(cfg.APIKey, cfg.Model), nil
	case "openai":
		if cfg.APIKey == "" {
			return nil, fmt.Errorf("openai STT requires api_key")
		}
		return stt.NewOpenAI(cfg.APIKey, cfg.Model), nil
	case "local":
		if cfg.ModelPath == "" {
			return nil, fmt.Errorf("local STT requires model_path")
		}
		return stt.NewLocal(cfg.BinaryPath, cfg.ModelPath, cfg.Language, cfg.Threads), nil
	default:
		return nil, fmt.Errorf("unknown STT provider: %s", cfg.Provider)
	}
}

func createTTS(cfg TTSConfig) (tts.TTS, error) {
	switch strings.ToLower(cfg.Provider) {
	case "openai":
		if cfg.APIKey == "" {
			return nil, fmt.Errorf("openai TTS requires api_key")
		}
		return tts.NewOpenAI(cfg.APIKey, cfg.Model), nil
	case "elevenlabs":
		if cfg.APIKey == "" {
			return nil, fmt.Errorf("elevenlabs TTS requires api_key")
		}
		return tts.NewElevenLabs(cfg.APIKey, cfg.Model), nil
	case "piper":
		if cfg.ModelPath == "" {
			return nil, fmt.Errorf("piper TTS requires model_path")
		}
		return tts.NewPiper(cfg.BinaryPath, cfg.ModelPath), nil
	default:
		return nil, fmt.Errorf("unknown TTS provider: %s", cfg.Provider)
	}
}
