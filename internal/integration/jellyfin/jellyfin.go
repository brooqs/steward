package jellyfin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/brooqs/steward/internal/integration"
	"github.com/brooqs/steward/internal/tools"
)

func init() {
	integration.Register("jellyfin", func() integration.Integration {
		return &JFIntegration{}
	})
}

// JFIntegration integrates with the Jellyfin media server API.
type JFIntegration struct {
	url     string
	apiKey  string
	enabled bool
	client  *http.Client
}

func (j *JFIntegration) Name() string       { return "jellyfin" }
func (j *JFIntegration) Enabled() bool      { return j.enabled }
func (j *JFIntegration) ToolPrefix() string  { return "jellyfin_" }

func (j *JFIntegration) LoadConfig(cfg map[string]any) error {
	u, _ := cfg["url"].(string)
	key, _ := cfg["api_key"].(string)
	if u == "" || key == "" {
		return fmt.Errorf("jellyfin requires 'url' and 'api_key'")
	}
	j.url = strings.TrimRight(u, "/")
	j.apiKey = key
	j.enabled = true
	j.client = &http.Client{Timeout: 10 * time.Second}
	return nil
}

func (j *JFIntegration) HealthCheck() bool {
	if !j.enabled {
		return false
	}
	_, err := j.apiGet("/System/Info/Public", nil)
	return err == nil
}

func (j *JFIntegration) GetTools() []tools.ToolSpec {
	if !j.enabled {
		return nil
	}
	return []tools.ToolSpec{
		{
			Name:        "jellyfin_search",
			Description: "Search the Jellyfin media library for movies, TV shows, music, etc.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":      map[string]any{"type": "string", "description": "Search term"},
					"media_type": map[string]any{"type": "string", "description": "Filter: Movie, Series, Episode, Audio"},
				},
				"required": []string{"query"},
			},
			Handler: j.search,
		},
		{
			Name:        "jellyfin_sessions",
			Description: "Get active Jellyfin playback sessions (who is watching what).",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
				"required":   []string{},
			},
			Handler: j.sessions,
		},
		{
			Name:        "jellyfin_recently_added",
			Description: "Get recently added items in the Jellyfin library.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"media_type": map[string]any{"type": "string", "description": "Movie or Series (default: Movie)"},
					"limit":      map[string]any{"type": "integer", "description": "Number of items (default: 5)"},
				},
				"required": []string{},
			},
			Handler: j.recentlyAdded,
		},
	}
}

func (j *JFIntegration) search(params map[string]any) (any, error) {
	query, _ := params["query"].(string)
	if query == "" {
		return nil, fmt.Errorf("query required")
	}
	qp := url.Values{"SearchTerm": {query}, "Limit": {"10"}, "Recursive": {"true"}}
	if mt, ok := params["media_type"].(string); ok && mt != "" {
		qp.Set("IncludeItemTypes", mt)
	}
	data, err := j.apiGet("/Items", qp)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	var resp map[string]any
	json.Unmarshal(data, &resp)
	items, _ := resp["Items"].([]any)
	var result []map[string]any
	for _, item := range items {
		m, _ := item.(map[string]any)
		overview, _ := m["Overview"].(string)
		if len(overview) > 200 {
			overview = overview[:200]
		}
		result = append(result, map[string]any{
			"id": m["Id"], "name": m["Name"], "type": m["Type"],
			"year": m["ProductionYear"], "overview": overview,
		})
	}
	return result, nil
}

func (j *JFIntegration) sessions(params map[string]any) (any, error) {
	data, err := j.apiGet("/Sessions", nil)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	var sessions []map[string]any
	json.Unmarshal(data, &sessions)
	var result []map[string]any
	for _, s := range sessions {
		entry := map[string]any{
			"user": s["UserName"], "client": s["Client"], "device": s["DeviceName"],
		}
		if np, ok := s["NowPlayingItem"].(map[string]any); ok {
			entry["now_playing"] = np["Name"]
		}
		result = append(result, entry)
	}
	return result, nil
}

func (j *JFIntegration) recentlyAdded(params map[string]any) (any, error) {
	mt := "Movie"
	if v, ok := params["media_type"].(string); ok && v != "" {
		mt = v
	}
	limit := "5"
	if v, ok := params["limit"].(float64); ok {
		limit = fmt.Sprintf("%d", int(v))
	}
	qp := url.Values{"IncludeItemTypes": {mt}, "Limit": {limit}, "Fields": {"Overview"}}
	data, err := j.apiGet("/Items/Latest", qp)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	var items []map[string]any
	json.Unmarshal(data, &items)
	var result []map[string]any
	for _, m := range items {
		overview, _ := m["Overview"].(string)
		if len(overview) > 200 {
			overview = overview[:200]
		}
		result = append(result, map[string]any{
			"name": m["Name"], "type": m["Type"],
			"year": m["ProductionYear"], "overview": overview,
		})
	}
	return result, nil
}

func (j *JFIntegration) apiGet(path string, params url.Values) ([]byte, error) {
	u := j.url + path
	if params == nil {
		params = url.Values{}
	}
	params.Set("api_key", j.apiKey)
	u += "?" + params.Encode()

	resp, err := j.client.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}
