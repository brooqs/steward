// Package whatsapp implements the WhatsApp webhook channel for Steward.
// It works with a whatsapp-web.js bridge (or similar) that sends incoming
// messages via POST and receives replies via a send API.
package whatsapp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/brooqs/steward/internal/config"
	"github.com/brooqs/steward/internal/core"
	"github.com/brooqs/steward/internal/voice"
)

// Channel handles WhatsApp communication via webhook.
type Channel struct {
	steward     *core.Steward
	voiceEngine *voice.Engine
	listenAddr  string
	bridgeURL   string
	secret      string
	httpClient  *http.Client
}

// New creates a new WhatsApp channel.
func New(steward *core.Steward, cfg config.WhatsAppConfig, ve *voice.Engine) (*Channel, error) {
	if cfg.BridgeURL == "" {
		return nil, fmt.Errorf("whatsapp bridge_url not set")
	}
	addr := cfg.ListenAddr
	if addr == "" {
		addr = "0.0.0.0:8765"
	}
	return &Channel{
		steward:     steward,
		voiceEngine: ve,
		listenAddr:  addr,
		bridgeURL:   strings.TrimRight(cfg.BridgeURL, "/"),
		secret:      cfg.WebhookSecret,
		httpClient:  &http.Client{Timeout: 15 * time.Second},
	}, nil
}

// Run starts the webhook HTTP server (blocking).
func (ch *Channel) Run(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/message", ch.handleWebhook)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:    ch.listenAddr,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	slog.Info("whatsapp webhook listening", "addr", ch.listenAddr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		return fmt.Errorf("webhook server: %w", err)
	}
	return nil
}

type webhookPayload struct {
	From          string `json:"from"`
	Message       string `json:"message"`
	AudioBase64   string `json:"audio_base64,omitempty"`
	AudioMimetype string `json:"audio_mimetype,omitempty"`
}

func (ch *Channel) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Security: validate webhook secret
	if ch.secret != "" {
		incoming := r.Header.Get("X-Webhook-Secret")
		if incoming != ch.secret {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	var payload webhookPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	sender := strings.TrimSpace(payload.From)
	if sender == "" {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ignored"))
		return
	}

	// Voice message → STT
	if payload.AudioBase64 != "" {
		slog.Info("whatsapp voice message", "from", sender)
		go ch.processVoiceAndReply(sender, payload.AudioBase64, payload.AudioMimetype)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
		return
	}

	// Text message
	text := strings.TrimSpace(payload.Message)
	if text == "" {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ignored"))
		return
	}

	slog.Info("whatsapp message", "from", sender, "text", truncate(text, 80))
	go ch.processAndReply(sender, text)

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func (ch *Channel) processVoiceAndReply(sender, audioB64, mimetype string) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Check if STT is available
	if ch.voiceEngine == nil || !ch.voiceEngine.HasSTT() {
		ch.SendReply(sender, "Sesli mesaj desteği aktif değil. Lütfen yazılı mesaj gönderin.")
		return
	}

	// Decode base64 audio
	audioData, err := base64.StdEncoding.DecodeString(audioB64)
	if err != nil {
		slog.Error("voice decode error", "error", err)
		ch.SendReply(sender, "Sesli mesaj okunamadı.")
		return
	}

	// Determine format from mimetype (audio/ogg; codecs=opus → ogg)
	format := "ogg"
	if strings.Contains(mimetype, "mp4") || strings.Contains(mimetype, "m4a") {
		format = "mp4"
	} else if strings.Contains(mimetype, "webm") {
		format = "webm"
	} else if strings.Contains(mimetype, "wav") {
		format = "wav"
	}

	// Transcribe
	text, err := ch.voiceEngine.Transcribe(ctx, audioData, format)
	if err != nil {
		slog.Error("STT error", "error", err)
		ch.SendReply(sender, "Sesli mesaj anlaşılamadı, tekrar deneyin.")
		return
	}

	text = strings.TrimSpace(text)
	if text == "" {
		ch.SendReply(sender, "Sesli mesajda konuşma algılanamadı.")
		return
	}

	slog.Info("voice transcribed", "from", sender, "text", truncate(text, 80))

	// Process transcribed text as a regular message
	ch.processAndReply(sender, text)
}

func (ch *Channel) processAndReply(sender, text string) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	sessionID := "whatsapp:" + sender
	response, err := ch.steward.Chat(ctx, sessionID, text)
	if err != nil {
		slog.Error("steward error", "error", err)
		response = "Sorry, something went wrong. Please try again."
	}

	ch.SendReply(sender, response)
}

func (ch *Channel) SendReply(to, message string) {
	payload := map[string]string{"to": to, "message": message}
	data, _ := json.Marshal(payload)

	resp, err := ch.httpClient.Post(ch.bridgeURL+"/send", "application/json", bytes.NewReader(data))
	if err != nil {
		slog.Error("bridge send error", "error", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		slog.Error("bridge send failed", "status", resp.StatusCode, "body", string(body))
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
