// Package provider also provides a factory function to create providers
// from configuration.
package provider

import (
	"fmt"
	"strings"
)

// New creates a Provider from the given configuration parameters.
func New(providerName, apiKey, model, baseURL string) (Provider, error) {
	name := strings.ToLower(providerName)
	switch name {
	case "claude":
		if apiKey == "" {
			return nil, fmt.Errorf("claude provider requires api_key")
		}
		return NewClaude(apiKey), nil

	case "openai", "groq", "openrouter", "ollama":
		return NewOpenAI(name, apiKey, baseURL), nil

	case "gemini":
		if apiKey == "" {
			return nil, fmt.Errorf("gemini provider requires api_key")
		}
		return NewGemini(apiKey), nil

	default:
		return nil, fmt.Errorf("unknown provider: %s", providerName)
	}
}
