// Package knowledge provides a tool result cache backed by embeddings.
// It stores tool outputs (entity lists, search results) as vectors,
// enabling semantic retrieval without re-calling the integration API.
package knowledge

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dgraph-io/badger/v4"

	"github.com/brooqs/steward/internal/embedding"
)

// Entry represents a cached piece of knowledge.
type Entry struct {
	Source     string    `json:"source"`      // e.g., "ha_list_entities", "spotify_search"
	Key       string    `json:"key"`         // unique key within source
	Content   string    `json:"content"`     // the actual data (JSON string)
	Summary   string    `json:"summary"`     // human-readable summary for context injection
	Exportable bool    `json:"exportable"`  // safe to export/share?
	CreatedAt time.Time `json:"created_at"`
	TTL       int64     `json:"ttl_seconds"` // 0 = never expires
}

// SearchResult is a knowledge search result with similarity score.
type SearchResult struct {
	Entry      Entry   `json:"entry"`
	Similarity float32 `json:"similarity"`
}

// Store caches tool results with embedding vectors for semantic retrieval.
//
// Key schema in BadgerDB:
//   kb:{source}:{key}   → JSON-encoded Entry
//   kbv:{source}:{key}  → binary float32 embedding vector
type Store struct {
	db       *badger.DB
	embedder embedding.Embedder
	mu       sync.RWMutex

	// Tools whose results should be cached
	cacheableTools map[string]CacheConfig
}

// CacheConfig defines caching behavior for a specific tool.
type CacheConfig struct {
	TTL        time.Duration
	Exportable bool
	Summarizer func(result string) string // extracts summary from tool result
}

// DefaultCacheConfigs returns caching configs for known tools.
func DefaultCacheConfigs() map[string]CacheConfig {
	return map[string]CacheConfig{
		"ha_list_entities": {
			TTL:        24 * time.Hour,
			Exportable: false,
			Summarizer: summarizeHAEntities,
		},
		"ha_sync_entities": {
			TTL:        24 * time.Hour,
			Exportable: false,
			Summarizer: summarizeHASyncEntities,
		},
		"ha_get_entity_state": {
			TTL:        5 * time.Minute,
			Exportable: false,
		},
		"spotify_search": {
			TTL:        1 * time.Hour,
			Exportable: false,
		},
		"jellyfin_search": {
			TTL:        1 * time.Hour,
			Exportable: false,
		},
	}
}

// NewStore creates a knowledge store using the given BadgerDB and embedder.
func NewStore(db *badger.DB, embedder embedding.Embedder) *Store {
	return &Store{
		db:             db,
		embedder:       embedder,
		cacheableTools: DefaultCacheConfigs(),
	}
}

// IsCacheable returns whether a tool's results should be cached.
func (s *Store) IsCacheable(toolName string) bool {
	_, ok := s.cacheableTools[toolName]
	return ok
}

// StoreResult caches a tool result with its embedding.
func (s *Store) StoreResult(ctx context.Context, toolName, inputKey, result string) error {
	if s.embedder == nil {
		return nil
	}

	cfg, ok := s.cacheableTools[toolName]
	if !ok {
		return nil
	}

	// Build summary for embedding
	summary := result
	if cfg.Summarizer != nil {
		summary = cfg.Summarizer(result)
	}
	if summary == "" || len(summary) < 5 {
		return nil
	}

	// Text to embed: tool name + summary
	embeddingText := toolName + ": " + truncate(summary, 500)

	vec, err := s.embedder.Embed(ctx, embeddingText)
	if err != nil {
		slog.Warn("knowledge embedding failed", "tool", toolName, "error", err)
		return err
	}

	entry := Entry{
		Source:     toolName,
		Key:       inputKey,
		Content:   truncate(result, 4096),
		Summary:   truncate(summary, 512),
		Exportable: cfg.Exportable,
		CreatedAt: time.Now(),
		TTL:       int64(cfg.TTL.Seconds()),
	}

	entryData, _ := json.Marshal(entry)
	vecData := float32ToBytes(vec)

	kbKey := fmt.Sprintf("kb:%s:%s", toolName, inputKey)
	vecKey := fmt.Sprintf("kbv:%s:%s", toolName, inputKey)

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.db.Update(func(txn *badger.Txn) error {
		e := badger.NewEntry([]byte(kbKey), entryData)
		if cfg.TTL > 0 {
			e = e.WithTTL(cfg.TTL)
		}
		if err := txn.SetEntry(e); err != nil {
			return err
		}
		ve := badger.NewEntry([]byte(vecKey), vecData)
		if cfg.TTL > 0 {
			ve = ve.WithTTL(cfg.TTL)
		}
		return txn.SetEntry(ve)
	})
}

// Query performs semantic search across cached knowledge.
func (s *Store) Query(ctx context.Context, query string, topK int) ([]SearchResult, error) {
	if s.embedder == nil {
		return nil, nil
	}
	if topK <= 0 {
		topK = 3
	}

	queryVec, err := s.embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embedding query: %w", err)
	}

	type scored struct {
		kbKey      string
		similarity float32
	}
	var matches []scored

	prefix := []byte("kbv:")
	err = s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			key := string(item.KeyCopy(nil))
			err := item.Value(func(val []byte) error {
				vec := bytesToFloat32(val)
				sim := embedding.CosineSimilarity(queryVec, vec)
				if sim >= 0.5 {
					kbKey := "kb:" + key[4:]
					matches = append(matches, scored{kbKey: kbKey, similarity: sim})
				}
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

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].similarity > matches[j].similarity
	})
	if len(matches) > topK {
		matches = matches[:topK]
	}

	var results []SearchResult
	err = s.db.View(func(txn *badger.Txn) error {
		for _, m := range matches {
			item, err := txn.Get([]byte(m.kbKey))
			if err != nil {
				continue
			}
			item.Value(func(val []byte) error {
				var entry Entry
				if err := json.Unmarshal(val, &entry); err != nil {
					return nil
				}
				results = append(results, SearchResult{
					Entry:      entry,
					Similarity: m.similarity,
				})
				return nil
			})
		}
		return nil
	})

	if len(results) > 0 {
		slog.Debug("knowledge query",
			"query", truncate(query, 50),
			"results", len(results),
			"top_score", results[0].Similarity,
		)
	}

	return results, err
}

// Clear removes cached knowledge, optionally filtered by source.
func (s *Store) Clear(source string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var prefixes [][]byte
	if source != "" {
		prefixes = [][]byte{
			[]byte("kb:" + source + ":"),
			[]byte("kbv:" + source + ":"),
		}
	} else {
		prefixes = [][]byte{[]byte("kb:"), []byte("kbv:")}
	}

	return s.db.Update(func(txn *badger.Txn) error {
		for _, prefix := range prefixes {
			opts := badger.DefaultIteratorOptions
			opts.PrefetchValues = false
			opts.Prefix = prefix
			it := txn.NewIterator(opts)

			var keys [][]byte
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				keys = append(keys, it.Item().KeyCopy(nil))
			}
			it.Close()

			for _, k := range keys {
				if err := txn.Delete(k); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// Count returns the number of cached knowledge entries.
func (s *Store) Count() int {
	count := 0
	s.db.View(func(txn *badger.Txn) error {
		prefix := []byte("kb:")
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

// Sources returns unique source names in the cache.
func (s *Store) Sources() []string {
	set := map[string]bool{}
	s.db.View(func(txn *badger.Txn) error {
		prefix := []byte("kb:")
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			key := string(it.Item().Key())
			parts := strings.SplitN(key[3:], ":", 2)
			if len(parts) > 0 {
				set[parts[0]] = true
			}
		}
		return nil
	})
	var sources []string
	for s := range set {
		sources = append(sources, s)
	}
	sort.Strings(sources)
	return sources
}

// FormatContext formats search results as context for the system prompt.
func FormatContext(results []SearchResult) string {
	if len(results) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n## Relevant Knowledge (from previous tool calls)\n")
	sb.WriteString("Use this information directly instead of calling tools again. Entity IDs and names below are accurate.\n")
	for _, r := range results {
		sb.WriteString(fmt.Sprintf("- [%s] %s\n", r.Entry.Source, r.Entry.Summary))
	}
	return sb.String()
}

// --- Summarizers ---

func summarizeHAEntities(result string) string {
	var entities []map[string]any
	if err := json.Unmarshal([]byte(result), &entities); err != nil {
		return result
	}
	var parts []string
	for _, e := range entities {
		eid, _ := e["entity_id"].(string)
		fname, _ := e["friendly_name"].(string)
		state, _ := e["state"].(string)
		if eid != "" {
			if fname != "" {
				parts = append(parts, fmt.Sprintf("%s (%s): %s", eid, fname, state))
			} else {
				parts = append(parts, fmt.Sprintf("%s: %s", eid, state))
			}
		}
	}
	return strings.Join(parts, "; ")
}

func summarizeHASyncEntities(result string) string {
	var data map[string]any
	if err := json.Unmarshal([]byte(result), &data); err != nil {
		return result
	}
	entities, _ := data["entities"].([]any)
	var parts []string
	for _, raw := range entities {
		e, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		eid, _ := e["entity_id"].(string)
		fname, _ := e["friendly_name"].(string)
		state, _ := e["state"].(string)
		if eid == "" {
			continue
		}
		desc := fmt.Sprintf("%s (%s): %s", eid, fname, state)
		// Add color modes if present
		if modes, ok := e["supported_color_modes"]; ok {
			desc += fmt.Sprintf(" [color_modes: %v]", modes)
		}
		if dc, ok := e["device_class"].(string); ok && dc != "" {
			desc += fmt.Sprintf(" [class: %s]", dc)
		}
		parts = append(parts, desc)
	}
	return strings.Join(parts, "; ")
}

// --- byte helpers (same as semantic.go) ---

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

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
