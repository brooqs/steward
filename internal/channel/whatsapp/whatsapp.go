// Package whatsapp implements the WhatsApp webhook channel for Steward.
// It works with a whatsapp-web.js bridge (or similar) that sends incoming
// messages via POST and receives replies via a send API.
package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/brooqs/steward/internal/config"
	"github.com/brooqs/steward/internal/core"
)

// Channel handles WhatsApp communication via webhook.
type Channel struct {
	steward   *core.Steward
	listenAddr string
	bridgeURL  string
	secret     string
	httpClient *http.Client
}

// New creates a new WhatsApp channel.
func New(steward *core.Steward, cfg config.WhatsAppConfig) (*Channel, error) {
	if cfg.BridgeURL == "" {
		return nil, fmt.Errorf("whatsapp bridge_url not set")
	}
	addr := cfg.ListenAddr
	if addr == "" {
		addr = "0.0.0.0:8765"
	}
	return &Channel{
		steward:    steward,
		listenAddr: addr,
		bridgeURL:  strings.TrimRight(cfg.BridgeURL, "/"),
		secret:     cfg.WebhookSecret,
		httpClient: &http.Client{Timeout: 15 * time.Second},
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
	From    string `json:"from"`
	Message string `json:"message"`
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
	text := strings.TrimSpace(payload.Message)
	if sender == "" || text == "" {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ignored"))
		return
	}

	slog.Info("whatsapp message", "from", sender, "text", truncate(text, 80))

	// Process async and return 200 immediately
	go ch.processAndReply(sender, text)

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
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

	ch.sendReply(sender, response)
}

func (ch *Channel) sendReply(to, message string) {
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
