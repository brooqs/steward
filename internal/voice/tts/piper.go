package tts

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// Piper implements TTS using the local Piper TTS engine.
// Piper is a fast, lightweight, offline TTS system that uses ONNX models.
//
// Install: https://github.com/rhasspy/piper
// Models: https://github.com/rhasspy/piper/blob/master/VOICES.md
type Piper struct {
	binaryPath string // path to piper binary
	modelPath  string // path to .onnx model file
}

// NewPiper creates a new local Piper TTS provider.
func NewPiper(binaryPath, modelPath string) *Piper {
	if binaryPath == "" {
		binaryPath = "piper" // assume it's in PATH
	}
	return &Piper{
		binaryPath: binaryPath,
		modelPath:  modelPath,
	}
}

func (p *Piper) Name() string { return "piper" }

func (p *Piper) Synthesize(ctx context.Context, text string, opts *Options) ([]byte, error) {
	if opts == nil {
		opts = DefaultOptions()
	}

	outputFile := fmt.Sprintf("/tmp/steward_tts_%d.wav", time.Now().UnixNano())
	defer os.Remove(outputFile)

	// Piper reads from stdin and writes WAV to file
	args := []string{
		"--model", p.modelPath,
		"--output_file", outputFile,
	}

	// Speaker ID if voice is specified as a number
	if opts.Voice != "" {
		args = append(args, "--speaker", opts.Voice)
	}

	// Length scale (inverse of speed)
	if opts.Speed > 0 && opts.Speed != 1.0 {
		lengthScale := 1.0 / opts.Speed
		args = append(args, "--length_scale", fmt.Sprintf("%.2f", lengthScale))
	}

	cmd := exec.CommandContext(ctx, p.binaryPath, args...)
	cmd.Stdin = bytes.NewReader([]byte(text))

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("piper failed: %w (stderr: %s)", err, stderr.String())
	}

	// Read the output WAV file
	audio, err := os.ReadFile(outputFile)
	if err != nil {
		return nil, fmt.Errorf("reading piper output: %w", err)
	}

	// Convert to requested format if needed
	if opts.Format != FormatWAV && opts.Format != "" {
		audio, err = convertAudio(ctx, audio, "wav", string(opts.Format))
		if err != nil {
			return nil, fmt.Errorf("converting audio: %w", err)
		}
	}

	return audio, nil
}

// convertAudio uses ffmpeg to convert between audio formats.
func convertAudio(ctx context.Context, input []byte, fromFormat, toFormat string) ([]byte, error) {
	inputFile := fmt.Sprintf("/tmp/steward_conv_%d.%s", time.Now().UnixNano(), fromFormat)
	outputFile := fmt.Sprintf("/tmp/steward_conv_%d.%s", time.Now().UnixNano(), toFormat)
	defer os.Remove(inputFile)
	defer os.Remove(outputFile)

	if err := os.WriteFile(inputFile, input, 0o600); err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-i", inputFile,
		"-y", outputFile,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("ffmpeg: %w (output: %s)", err, string(out))
	}

	return os.ReadFile(outputFile)
}
