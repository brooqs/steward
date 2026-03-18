// Package memory provides the conversation memory store interface and
// implementations (BadgerDB, PostgreSQL).
package memory

import "time"

// Message represents a stored conversation message.
type Message struct {
	SessionID string    `json:"session_id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// Store is the interface that memory backends must implement.
type Store interface {
	// SaveMessage persists a message in the given session.
	SaveMessage(sessionID, role, content string) error

	// GetRecentMessages returns the most recent messages for a session.
	// If limit <= 0, use the implementation's default.
	GetRecentMessages(sessionID string, limit int) ([]Message, error)

	// ClearSession deletes all messages in a session.
	ClearSession(sessionID string) error

	// Close releases any resources held by the store.
	Close() error
}
