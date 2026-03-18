package memory

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

// PostgresStore implements Store using PostgreSQL.
// Requires the pgvector extension for optional vector search.
type PostgresStore struct {
	db           *sql.DB
	defaultLimit int
}

// NewPostgresStore opens a connection to PostgreSQL and ensures
// the schema is ready.
func NewPostgresStore(connURL string, defaultLimit int) (*PostgresStore, error) {
	if defaultLimit <= 0 {
		defaultLimit = 10
	}

	db, err := sql.Open("postgres", connURL)
	if err != nil {
		return nil, fmt.Errorf("opening postgres: %w", err)
	}

	// Connection pool settings
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("pinging postgres: %w", err)
	}

	store := &PostgresStore{db: db, defaultLimit: defaultLimit}
	if err := store.migrate(); err != nil {
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return store, nil
}

// migrate creates the necessary tables and extensions.
func (p *PostgresStore) migrate() error {
	queries := []string{
		// Core messages table
		`CREATE TABLE IF NOT EXISTS messages (
			id         BIGSERIAL PRIMARY KEY,
			session_id TEXT NOT NULL,
			role       TEXT NOT NULL,
			content    TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, created_at)`,

		// Try to enable pgvector extension (will silently fail if not installed)
		`CREATE EXTENSION IF NOT EXISTS vector`,

		// Semantic memory table with vector column (384 dims for MiniLM)
		`CREATE TABLE IF NOT EXISTS semantic_memories (
			id         BIGSERIAL PRIMARY KEY,
			session_id TEXT NOT NULL,
			role       TEXT NOT NULL,
			content    TEXT NOT NULL,
			embedding  vector(384),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_semantic_session ON semantic_memories(session_id)`,
	}

	for _, q := range queries {
		if _, err := p.db.Exec(q); err != nil {
			// Don't fail on vector extension errors — it's optional
			if q == `CREATE EXTENSION IF NOT EXISTS vector` {
				continue
			}
			// Don't fail on semantic_memories if vector is not available
			if q[:30] == `CREATE TABLE IF NOT EXISTS sem` {
				continue
			}
			return fmt.Errorf("migration failed: %w (query: %s)", err, q[:50])
		}
	}
	return nil
}

func (p *PostgresStore) SaveMessage(sessionID, role, content string) error {
	_, err := p.db.Exec(
		`INSERT INTO messages (session_id, role, content) VALUES ($1, $2, $3)`,
		sessionID, role, content,
	)
	return err
}

func (p *PostgresStore) GetRecentMessages(sessionID string, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = p.defaultLimit
	}

	// Get the last N messages ordered by creation time
	rows, err := p.db.Query(
		`SELECT session_id, role, content, created_at
		 FROM messages
		 WHERE session_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2`,
		sessionID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("querying messages: %w", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.SessionID, &m.Role, &m.Content, &m.Timestamp); err != nil {
			return nil, fmt.Errorf("scanning message: %w", err)
		}
		messages = append(messages, m)
	}

	// Reverse to get chronological order
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	return messages, nil
}

func (p *PostgresStore) ClearSession(sessionID string) error {
	_, err := p.db.Exec(`DELETE FROM messages WHERE session_id = $1`, sessionID)
	if err != nil {
		return err
	}
	// Also clear semantic memories
	p.db.Exec(`DELETE FROM semantic_memories WHERE session_id = $1`, sessionID)
	return nil
}

func (p *PostgresStore) Close() error {
	return p.db.Close()
}
