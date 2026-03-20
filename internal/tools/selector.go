package tools

import (
	"context"
	"log/slog"
	"sort"
	"sync"

	"github.com/brooqs/steward/internal/embedding"
	"github.com/brooqs/steward/internal/provider"
)

const defaultTopK = 15

// toolEmbedding stores a tool's precomputed embedding vector.
type toolEmbedding struct {
	Name      string
	Schema    provider.ToolSchema
	Vector    []float32
}

// ToolSelector provides intelligent tool selection by matching user messages
// to tool descriptions via embedding similarity. If no embedder is available,
// it falls back to returning all tools.
type ToolSelector struct {
	registry  *Registry
	embedder  embedding.Embedder
	topK      int

	mu         sync.RWMutex
	toolIndex  []toolEmbedding // precomputed tool embeddings
	pinnedSet  map[string]bool // always-include tools (e.g., shell, web)
}

// NewToolSelector creates a ToolSelector. If embedder is nil, all tools are
// always returned (backward compatible).
func NewToolSelector(registry *Registry, embedder embedding.Embedder, topK int) *ToolSelector {
	if topK <= 0 {
		topK = defaultTopK
	}
	return &ToolSelector{
		registry:  registry,
		embedder:  embedder,
		topK:      topK,
		pinnedSet: map[string]bool{
			"shell_exec":      true,
			"web_fetch":       true,
			"web_search":      true,
			"ha_call_service": true,
		},
	}
}

// IndexTools pre-computes embeddings for all registered tool descriptions.
// Call this at startup and after hot-reload.
func (ts *ToolSelector) IndexTools(ctx context.Context) error {
	if ts.embedder == nil {
		return nil
	}

	schemas := ts.registry.GetSchemas()
	if len(schemas) == 0 {
		return nil
	}

	// Build description strings for embedding
	texts := make([]string, len(schemas))
	for i, s := range schemas {
		texts[i] = s.Name + ": " + s.Description
	}

	// Batch embed all tool descriptions
	vectors, err := ts.embedder.EmbedBatch(ctx, texts)
	if err != nil {
		slog.Error("failed to embed tool descriptions", "error", err)
		return err
	}

	// Build the index
	index := make([]toolEmbedding, len(schemas))
	for i, s := range schemas {
		index[i] = toolEmbedding{
			Name:   s.Name,
			Schema: s,
			Vector: vectors[i],
		}
	}

	ts.mu.Lock()
	ts.toolIndex = index
	ts.mu.Unlock()

	slog.Info("tool embeddings indexed", "tools", len(index), "dimensions", ts.embedder.Dimensions())
	return nil
}

// SelectTools returns the most relevant tool schemas for a given user message.
// pinned contains tool names that must be included (e.g., tools already used in
// the current turn). Returns all tools if no embedder is configured.
func (ts *ToolSelector) SelectTools(ctx context.Context, userMessage string, pinned []string) []provider.ToolSchema {
	// No embedder → return all tools (backward compatible)
	if ts.embedder == nil {
		return ts.registry.GetSchemas()
	}

	ts.mu.RLock()
	index := ts.toolIndex
	ts.mu.RUnlock()

	// If index is empty, return all tools
	if len(index) == 0 {
		return ts.registry.GetSchemas()
	}

	// Embed the user message
	queryVec, err := ts.embedder.Embed(ctx, userMessage)
	if err != nil {
		slog.Warn("tool selection embedding failed, using all tools", "error", err)
		return ts.registry.GetSchemas()
	}

	// Build pinned set for this call
	pinnedMap := make(map[string]bool, len(ts.pinnedSet)+len(pinned))
	for k, v := range ts.pinnedSet {
		pinnedMap[k] = v
	}
	for _, name := range pinned {
		pinnedMap[name] = true
	}

	// Score all tools
	type scored struct {
		idx   int
		score float32
	}
	scores := make([]scored, len(index))
	for i, te := range index {
		scores[i] = scored{idx: i, score: embedding.CosineSimilarity(queryVec, te.Vector)}
	}

	// Sort by score descending
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	// Collect results: pinned tools first, then top-K by score
	selected := make(map[string]provider.ToolSchema, ts.topK)

	// Add pinned tools
	for _, te := range index {
		if pinnedMap[te.Name] {
			selected[te.Name] = te.Schema
		}
	}

	// Add top-K scored tools
	for _, s := range scores {
		if len(selected) >= ts.topK {
			break
		}
		te := index[s.idx]
		if _, exists := selected[te.Name]; !exists {
			selected[te.Name] = te.Schema
		}
	}

	// Convert to slice
	result := make([]provider.ToolSchema, 0, len(selected))
	selectedNames := make([]string, 0, len(selected))
	for name, schema := range selected {
		result = append(result, schema)
		selectedNames = append(selectedNames, name)
	}

	slog.Info("tool selection",
		"query", truncateStr(userMessage, 60),
		"selected", len(result),
		"total", len(index),
		"top_score", scores[0].score,
		"tools", selectedNames,
	)

	return result
}

// Registry returns the underlying registry for dispatch.
func (ts *ToolSelector) Registry() *Registry {
	return ts.registry
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
