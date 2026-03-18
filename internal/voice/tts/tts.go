// Package tts defines the Text-to-Speech interface and provides
// implementations for OpenAI TTS, ElevenLabs, and local Piper TTS.
package tts

import "context"

// AudioFormat represents the output audio format.
type AudioFormat string

const (
	FormatMP3  AudioFormat = "mp3"
	FormatWAV  AudioFormat = "wav"
	FormatOGG  AudioFormat = "ogg"
	FormatOpus AudioFormat = "opus"
)

// TTS is the interface for text-to-speech providers.
type TTS interface {
	// Synthesize converts text to audio and returns the raw audio bytes.
	Synthesize(ctx context.Context, text string, opts *Options) ([]byte, error)
	// Name returns the provider identifier.
	Name() string
}

// Options holds optional TTS parameters.
type Options struct {
	Voice  string      // voice ID or name
	Format AudioFormat // output format
	Speed  float64     // playback speed multiplier (0.5 - 2.0)
}

// DefaultOptions returns sensible defaults.
func DefaultOptions() *Options {
	return &Options{
		Voice:  "",
		Format: FormatMP3,
		Speed:  1.0,
	}
}
