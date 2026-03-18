package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Local implements STT using a locally installed whisper.cpp binary.
// This provides fully offline speech recognition.
//
// Requires: whisper-cpp binary installed and a GGML model file.
// Install: https://github.com/ggerganov/whisper.cpp
type Local struct {
	binaryPath string // path to whisper.cpp main binary
	modelPath  string // path to GGML model file
	language   string // language hint (e.g., "tr", "en", "auto")
	threads    int
}

// NewLocal creates a new local whisper.cpp STT provider.
func NewLocal(binaryPath, modelPath, language string, threads int) *Local {
	if binaryPath == "" {
		binaryPath = "whisper-cpp" // assume it's in PATH
	}
	if language == "" {
		language = "auto"
	}
	if threads <= 0 {
		threads = 4
	}
	return &Local{
		binaryPath: binaryPath,
		modelPath:  modelPath,
		language:   language,
		threads:    threads,
	}
}

func (l *Local) Name() string { return "local" }

func (l *Local) Transcribe(ctx context.Context, audioData []byte, format string) (*Result, error) {
	// whisper.cpp expects WAV input — if we get a different format,
	// we'll need ffmpeg to convert. For now, write to a temp file.
	tmpFile := fmt.Sprintf("/tmp/steward_stt_%d.%s", time.Now().UnixNano(), format)
	wavFile := fmt.Sprintf("/tmp/steward_stt_%d.wav", time.Now().UnixNano())

	// Write audio to temp file
	if err := writeFile(tmpFile, audioData); err != nil {
		return nil, fmt.Errorf("writing temp audio: %w", err)
	}
	defer removeFile(tmpFile)
	defer removeFile(wavFile)

	// Convert to WAV if needed (16kHz mono, required by whisper.cpp)
	if format != "wav" {
		cmd := exec.CommandContext(ctx, "ffmpeg",
			"-i", tmpFile,
			"-ar", "16000",
			"-ac", "1",
			"-c:a", "pcm_s16le",
			"-y", wavFile,
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("ffmpeg conversion failed: %w (output: %s)", err, string(out))
		}
	} else {
		wavFile = tmpFile
	}

	// Run whisper.cpp
	args := []string{
		"-m", l.modelPath,
		"-f", wavFile,
		"-t", fmt.Sprintf("%d", l.threads),
		"-oj", // output JSON
		"--no-timestamps",
	}
	if l.language != "auto" {
		args = append(args, "-l", l.language)
	}

	cmd := exec.CommandContext(ctx, l.binaryPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("whisper.cpp failed: %w (stderr: %s)", err, stderr.String())
	}

	// Parse JSON output
	output := stdout.String()

	// Try JSON parse first
	var whisperOut struct {
		Transcription []struct {
			Text string `json:"text"`
		} `json:"transcription"`
	}
	if err := json.Unmarshal([]byte(output), &whisperOut); err == nil && len(whisperOut.Transcription) > 0 {
		var texts []string
		for _, t := range whisperOut.Transcription {
			texts = append(texts, strings.TrimSpace(t.Text))
		}
		return &Result{
			Text:     strings.Join(texts, " "),
			Language: l.language,
		}, nil
	}

	// Fallback: read plain text output
	text := strings.TrimSpace(output)
	if text == "" {
		text = strings.TrimSpace(stderr.String())
	}

	return &Result{
		Text:     text,
		Language: l.language,
	}, nil
}

func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}

func removeFile(path string) {
	os.Remove(path)
}
