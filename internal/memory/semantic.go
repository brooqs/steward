package memory

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/dgraph-io/badger/v4"

	"github.com/brooqs/steward/internal/embedding"
)

// SemanticEntry represents a stored memory with its embedding vector.
type SemanticEntry struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Content   string    `json:"content"`
	Role      string    `json:"role"`
	Timestamp time.Time `json:"timestamp"`
}

// SearchResult is a semantic search result with similarity score.
type SearchResult struct {
	Entry      SemanticEntry `json:"entry"`
	Similarity float32       `json:"similarity"`
}

// SemanticStore extends the basic memory store with embedding-based
// semantic search. It stores embeddings alongside messages in BadgerDB.
//
// Key schema:
//
//	vec:{session_id}:{timestamp_ns} → binary float32 embedding
//	sem:{session_id}:{timestamp_ns} → JSON-encoded SemanticEntry
type SemanticStore struct {
	db       *badger.DB
	embedder embedding.Embedder
	dims     int
}

// NewSemanticStore wraps an existing BadgerDB with semantic search capabilities.
func NewSemanticStore(db *badger.DB, embedder embedding.Embedder) *SemanticStore {
	return &SemanticStore{
		db:       db,
		embedder: embedder,
		dims:     embedder.Dimensions(),
	}
}

// Store saves a message with its embedding for semantic retrieval.
func (s *SemanticStore) Store(ctx context.Context, sessionID, role, content string) error {
	// Generate embedding
	vec, err := s.embedder.Embed(ctx, content)
	if err != nil {
		slog.Error("embedding generation failed", "error", err)
		return fmt.Errorf("generating embedding: %w", err)
	}

	now := time.Now()
	entry := SemanticEntry{
		ID:        fmt.Sprintf("%s_%d", sessionID, now.UnixNano()),
		SessionID: sessionID,
		Content:   content,
		Role:      role,
		Timestamp: now,
	}

	entryData, _ := json.Marshal(entry)
	vecData := float32ToBytes(vec)

	return s.db.Update(func(txn *badger.Txn) error {
		semKey := []byte(fmt.Sprintf("sem:%s:%020d", sessionID, now.UnixNano()))
		if err := txn.Set(semKey, entryData); err != nil {
			return err
		}
		vecKey := []byte(fmt.Sprintf("vec:%s:%020d", sessionID, now.UnixNano()))
		return txn.Set(vecKey, vecData)
	})
}

// Search performs semantic similarity search across all stored memories.
// Returns the top-k most similar entries.
func (s *SemanticStore) Search(ctx context.Context, query string, topK int) ([]SearchResult, error) {
	if topK <= 0 {
		topK = 5
	}

	// Embed the query
	queryVec, err := s.embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embedding query: %w", err)
	}

	// Brute-force scan all vectors
	type vecEntry struct {
		key []byte
		vec []float32
	}
	var vectors []vecEntry

	err = s.db.View(func(txn *badger.Txn) error {
		prefix := []byte("vec:")
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			key := item.KeyCopy(nil)
			err := item.Value(func(val []byte) error {
				vec := bytesToFloat32(val)
				vectors = append(vectors, vecEntry{key: key, vec: vec})
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scanning vectors: %w", err)
	}

	// Compute similarities
	type scored struct {
		semKey     string
		similarity float32
	}
	var scored_results []scored

	for _, v := range vectors {
		sim := embedding.CosineSimilarity(queryVec, v.vec)
		// Convert vec key to sem key: "vec:..." → "sem:..."
		semKey := "sem:" + string(v.key[4:])
		scored_results = append(scored_results, scored{semKey: semKey, similarity: sim})
	}

	// Sort by similarity descending
	sort.Slice(scored_results, func(i, j int) bool {
		return scored_results[i].similarity > scored_results[j].similarity
	})

	// Take top-k
	if len(scored_results) > topK {
		scored_results = scored_results[:topK]
	}

	// Fetch the actual entries
	var results []SearchResult
	err = s.db.View(func(txn *badger.Txn) error {
		for _, sr := range scored_results {
			if sr.similarity < 0.1 { // skip very low similarity
				continue
			}
			item, err := txn.Get([]byte(sr.semKey))
			if err != nil {
				continue
			}
			err = item.Value(func(val []byte) error {
				var entry SemanticEntry
				if err := json.Unmarshal(val, &entry); err != nil {
					return nil // skip corrupt entries
				}
				results = append(results, SearchResult{
					Entry:      entry,
					Similarity: sr.similarity,
				})
				return nil
			})
			if err != nil {
				continue
			}
		}
		return nil
	})

	return results, err
}

// SearchInSession performs semantic search within a specific session.
func (s *SemanticStore) SearchInSession(ctx context.Context, sessionID, query string, topK int) ([]SearchResult, error) {
	if topK <= 0 {
		topK = 5
	}

	queryVec, err := s.embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embedding query: %w", err)
	}

	prefix := []byte("vec:" + sessionID + ":")
	type scored struct {
		semKey     string
		similarity float32
	}
	var scored_results []scored

	err = s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			key := item.KeyCopy(nil)
			err := item.Value(func(val []byte) error {
				vec := bytesToFloat32(val)
				sim := embedding.CosineSimilarity(queryVec, vec)
				semKey := "sem:" + string(key[4:])
				scored_results = append(scored_results, scored{semKey: semKey, similarity: sim})
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(scored_results, func(i, j int) bool {
		return scored_results[i].similarity > scored_results[j].similarity
	})
	if len(scored_results) > topK {
		scored_results = scored_results[:topK]
	}

	var results []SearchResult
	err = s.db.View(func(txn *badger.Txn) error {
		for _, sr := range scored_results {
			if sr.similarity < 0.1 {
				continue
			}
			item, err := txn.Get([]byte(sr.semKey))
			if err != nil {
				continue
			}
			item.Value(func(val []byte) error {
				var entry SemanticEntry
				if err := json.Unmarshal(val, &entry); err != nil {
					return nil
				}
				results = append(results, SearchResult{
					Entry:      entry,
					Similarity: sr.similarity,
				})
				return nil
			})
		}
		return nil
	})

	return results, err
}

// Count returns the number of stored semantic entries.
func (s *SemanticStore) Count() int {
	count := 0
	s.db.View(func(txn *badger.Txn) error {
		prefix := []byte("sem:")
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			count++
		}
		return nil
	})
	return count
}

// float32 ↔ byte conversion helpers
func float32ToBytes(vec []float32) []byte {
	buf := make([]byte, len(vec)*4)
	for i, v := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

func bytesToFloat32(data []byte) []float32 {
	n := len(data) / 4
	vec := make([]float32, n)
	for i := 0; i < n; i++ {
		vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return vec
}
