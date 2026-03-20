package memory

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/dgraph-io/badger/v4"
)

// BadgerStore implements Store using BadgerDB.
//
// Key schema:
//
//	msg:{session_id}:{timestamp_ns} → JSON-encoded Message
//	ses:{session_id}                → session metadata (created_at)
type BadgerStore struct {
	db           *badger.DB
	defaultLimit int
}

// NewBadgerStore opens or creates a BadgerDB at the given path.
func NewBadgerStore(dataDir string, defaultLimit int) (*BadgerStore, error) {
	if defaultLimit <= 0 {
		defaultLimit = 10
	}

	opts := badger.DefaultOptions(dataDir).
		WithLogger(nil) // suppress badger's default logging

	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("opening badger: %w", err)
	}

	return &BadgerStore{
		db:           db,
		defaultLimit: defaultLimit,
	}, nil
}

func (b *BadgerStore) SaveMessage(sessionID, role, content string) error {
	msg := Message{
		SessionID: sessionID,
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	key := msgKey(sessionID, msg.Timestamp)

	return b.db.Update(func(txn *badger.Txn) error {
		// Save message
		if err := txn.Set(key, data); err != nil {
			return err
		}
		// Ensure session exists
		sesKey := []byte("ses:" + sessionID)
		_, err := txn.Get(sesKey)
		if err == badger.ErrKeyNotFound {
			sesMeta, _ := json.Marshal(map[string]any{
				"created_at": time.Now().Unix(),
			})
			return txn.Set(sesKey, sesMeta)
		}
		return nil
	})
}

func (b *BadgerStore) GetRecentMessages(sessionID string, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = b.defaultLimit
	}

	prefix := []byte("msg:" + sessionID + ":")
	var messages []Message

	err := b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()

		// Collect all messages for this session
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				var msg Message
				if err := json.Unmarshal(val, &msg); err != nil {
					return err
				}
				messages = append(messages, msg)
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reading messages: %w", err)
	}

	// Sort by timestamp ascending
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].Timestamp.Before(messages[j].Timestamp)
	})

	// Return only the last N messages
	if len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}

	return messages, nil
}

func (b *BadgerStore) ClearSession(sessionID string) error {
	prefix := []byte("msg:" + sessionID + ":")

	return b.db.Update(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()

		var keysToDelete [][]byte
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			key := it.Item().KeyCopy(nil)
			keysToDelete = append(keysToDelete, key)
		}

		for _, key := range keysToDelete {
			if err := txn.Delete(key); err != nil {
				return err
			}
		}
		return nil
	})
}

func (b *BadgerStore) Close() error {
	return b.db.Close()
}

// DB returns the underlying BadgerDB instance for sharing with other stores.
func (b *BadgerStore) DB() *badger.DB {
	return b.db
}

// msgKey generates a BadgerDB key for a message.
// Format: msg:{session_id}:{timestamp_ns}
func msgKey(sessionID string, t time.Time) []byte {
	return []byte(fmt.Sprintf("msg:%s:%020d", sessionID, t.UnixNano()))
}
