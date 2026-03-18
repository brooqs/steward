// Package telegram implements the Telegram bot channel for Steward.
package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/brooqs/steward/internal/config"
	"github.com/brooqs/steward/internal/core"
)

// Channel handles Telegram bot communication.
type Channel struct {
	steward    *core.Steward
	token      string
	allowedIDs map[int64]bool // empty = allow all (less secure)
}

// New creates a new Telegram channel.
func New(steward *core.Steward, cfg config.TelegramConfig) (*Channel, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("telegram token not set")
	}

	allowed := make(map[int64]bool, len(cfg.AllowedIDs))
	for _, id := range cfg.AllowedIDs {
		allowed[id] = true
	}

	return &Channel{
		steward:    steward,
		token:      cfg.Token,
		allowedIDs: allowed,
	}, nil
}

// Run starts the Telegram bot (blocking).
func (ch *Channel) Run(ctx context.Context) error {
	bot, err := tgbotapi.NewBotAPI(ch.token)
	if err != nil {
		return fmt.Errorf("creating bot: %w", err)
	}

	slog.Info("telegram bot started", "username", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			bot.StopReceivingUpdates()
			return nil
		case update := <-updates:
			if update.Message == nil {
				continue
			}
			go ch.handleUpdate(ctx, bot, update)
		}
	}
}

func (ch *Channel) handleUpdate(ctx context.Context, bot *tgbotapi.BotAPI, update tgbotapi.Update) {
	msg := update.Message
	chatID := msg.Chat.ID
	userID := msg.From.ID

	// Security: check if user is allowed
	if len(ch.allowedIDs) > 0 && !ch.allowedIDs[userID] && !ch.allowedIDs[chatID] {
		slog.Warn("unauthorized access attempt",
			"user_id", userID,
			"chat_id", chatID,
			"username", msg.From.UserName,
		)
		reply := tgbotapi.NewMessage(chatID, "⛔ Unauthorized. Contact the administrator.")
		bot.Send(reply)
		return
	}

	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return
	}

	// Handle commands
	if msg.IsCommand() {
		ch.handleCommand(bot, msg)
		return
	}

	slog.Info("telegram message",
		"chat_id", chatID,
		"user_id", userID,
		"text", truncate(text, 80),
	)

	// Send typing indicator
	typing := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
	bot.Send(typing)

	// Process with Steward
	sessionID := fmt.Sprintf("telegram:%d", chatID)
	response, err := ch.steward.Chat(ctx, sessionID, text)
	if err != nil {
		slog.Error("steward error", "error", err)
		response = "Sorry, something went wrong. Please try again."
	}

	// Telegram has a 4096 char limit — split if needed
	for _, chunk := range splitMessage(response, 4096) {
		reply := tgbotapi.NewMessage(chatID, chunk)
		reply.ParseMode = "Markdown"
		if _, err := bot.Send(reply); err != nil {
			// Retry without markdown if parsing fails
			reply.ParseMode = ""
			bot.Send(reply)
		}
	}
}

func (ch *Channel) handleCommand(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	var response string

	switch msg.Command() {
	case "start":
		response = "👋 Hello! I'm Steward, your AI personal assistant.\nSend me a message and I'll do my best to help."
	case "clear":
		sessionID := fmt.Sprintf("telegram:%d", chatID)
		if err := ch.steward.Memory().ClearSession(sessionID); err != nil {
			response = "❌ Failed to clear history."
		} else {
			response = "✅ Conversation history cleared."
		}
	case "status":
		toolList := ch.steward.Registry().ListTools()
		if len(toolList) > 0 {
			response = "🔧 Active tools:\n"
			for _, t := range toolList {
				response += fmt.Sprintf("  • %s\n", t)
			}
		} else {
			response = "No tools loaded."
		}
	default:
		response = "Unknown command. Available: /start, /clear, /status"
	}

	reply := tgbotapi.NewMessage(chatID, response)
	bot.Send(reply)
}

// splitMessage splits a message into chunks of maxLen.
func splitMessage(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}
	var chunks []string
	for len(text) > 0 {
		if len(text) <= maxLen {
			chunks = append(chunks, text)
			break
		}
		// Try to split at newline
		idx := strings.LastIndex(text[:maxLen], "\n")
		if idx < maxLen/2 {
			idx = maxLen
		}
		chunks = append(chunks, text[:idx])
		text = text[idx:]
	}
	return chunks
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
