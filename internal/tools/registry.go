// Package tools provides the tool registry for LLM tool-use / function calling.
// It is thread-safe to support hot-reload of integrations.
package tools

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/brooqs/steward/internal/provider"
)

// HandlerFunc is the function signature for tool handlers.
// It receives the tool input parameters and returns a result or error.
type HandlerFunc func(params map[string]any) (any, error)

// ToolSpec describes a tool to register.
type ToolSpec struct {
	Name        string
	Description string
	Parameters  map[string]any // JSON Schema for input
	Handler     HandlerFunc
}

// Tool is an internal representation of a registered tool.
type Tool struct {
	spec ToolSpec
}

// Registry holds all registered tools and provides thread-safe access.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]*Tool
}

// NewRegistry creates a new empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]*Tool),
	}
}

// Register adds a single tool to the registry.
func (r *Registry) Register(spec ToolSpec) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[spec.Name] = &Tool{spec: spec}
	slog.Debug("registered tool", "name", spec.Name)
}

// Unregister removes a tool by name.
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tools, name)
	slog.Debug("unregistered tool", "name", name)
}

// UnregisterPrefix removes all tools whose name starts with the given prefix.
// Used when unloading an integration (e.g., prefix "ha_" for Home Assistant).
func (r *Registry) UnregisterPrefix(prefix string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for name := range r.tools {
		if len(name) >= len(prefix) && name[:len(prefix)] == prefix {
			delete(r.tools, name)
			slog.Debug("unregistered tool", "name", name)
		}
	}
}

// RegisterAll adds multiple tools at once.
func (r *Registry) RegisterAll(specs []ToolSpec) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, spec := range specs {
		r.tools[spec.Name] = &Tool{spec: spec}
		slog.Debug("registered tool", "name", spec.Name)
	}
}

// GetSchemas returns all tool schemas formatted for the LLM provider API.
func (r *Registry) GetSchemas() []provider.ToolSchema {
	r.mu.RLock()
	defer r.mu.RUnlock()

	schemas := make([]provider.ToolSchema, 0, len(r.tools))
	for _, t := range r.tools {
		schemas = append(schemas, provider.ToolSchema{
			Name:        t.spec.Name,
			Description: t.spec.Description,
			InputSchema: t.spec.Parameters,
		})
	}
	return schemas
}

// Dispatch calls the named tool with the given input and returns the result.
func (r *Registry) Dispatch(name string, input map[string]any) (string, error) {
	r.mu.RLock()
	tool, ok := r.tools[name]
	r.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}

	slog.Info("dispatching tool", "name", name, "input", input)

	result, err := tool.spec.Handler(input)
	if err != nil {
		slog.Error("tool failed", "name", name, "error", err)
		errResult := map[string]string{"error": err.Error()}
		data, _ := json.Marshal(errResult)
		return string(data), nil // Return error as tool result, not as Go error
	}

	return serializeResult(result), nil
}

// ListTools returns the names of all registered tools.
func (r *Registry) ListTools() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// Count returns the number of registered tools.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}

// serializeResult converts any result to a JSON string.
func serializeResult(result any) string {
	switch v := result.(type) {
	case string:
		return v
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(data)
	}
}
