// Package stt defines the Speech-to-Text interface and provides
// implementations for Groq Whisper, OpenAI Whisper, and local whisper.cpp.
package stt

import "context"

// Result holds the transcription output.
type Result struct {
	Text     string  `json:"text"`
	Language string  `json:"language,omitempty"`
	Duration float64 `json:"duration,omitempty"` // audio duration in seconds
}

// STT is the interface for speech-to-text providers.
type STT interface {
	// Transcribe converts audio data to text.
	// audioData is the raw audio bytes, format is the audio format (e.g., "wav", "mp3", "ogg", "webm").
	Transcribe(ctx context.Context, audioData []byte, format string) (*Result, error)
	// Name returns the provider identifier.
	Name() string
}
