package jellyfin

import (
	"bytes"
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
		{
			Name:        "jellyfin_libraries",
			Description: "List all media libraries (Movies, TV Shows, Music, etc.) in Jellyfin.",
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
			Handler:     j.libraries,
		},
		{
			Name:        "jellyfin_item_details",
			Description: "Get detailed information about a specific Jellyfin item (movie, episode, etc.).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"item_id": map[string]any{"type": "string", "description": "Jellyfin item ID (from search results)"},
				},
				"required": []string{"item_id"},
			},
			Handler: j.itemDetails,
		},
		{
			Name:        "jellyfin_play",
			Description: "Start playing an item on a specific Jellyfin session/device.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"session_id": map[string]any{"type": "string", "description": "Target session ID (from jellyfin_sessions)"},
					"item_id":    map[string]any{"type": "string", "description": "Item ID to play (from jellyfin_search)"},
				},
				"required": []string{"session_id", "item_id"},
			},
			Handler: j.play,
		},
		{
			Name:        "jellyfin_playback_control",
			Description: "Control playback on a Jellyfin session (pause, resume, stop, next, previous).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"session_id": map[string]any{"type": "string", "description": "Target session ID"},
					"command":    map[string]any{"type": "string", "description": "Command: PlayPause, Stop, NextTrack, PreviousTrack, Seek", "enum": []string{"PlayPause", "Stop", "NextTrack", "PreviousTrack"}},
				},
				"required": []string{"session_id", "command"},
			},
			Handler: j.playbackControl,
		},
		{
			Name:        "jellyfin_stream_url",
			Description: "Generate a direct stream URL for a Jellyfin item (useful for casting or external playback).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"item_id": map[string]any{"type": "string", "description": "Jellyfin item ID to stream"},
				},
				"required": []string{"item_id"},
			},
			Handler: j.streamURL,
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

func (j *JFIntegration) libraries(params map[string]any) (any, error) {
	data, err := j.apiGet("/Library/VirtualFolders", nil)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	var folders []map[string]any
	json.Unmarshal(data, &folders)
	var result []map[string]any
	for _, f := range folders {
		result = append(result, map[string]any{
			"name":          f["Name"],
			"collection_type": f["CollectionType"],
			"item_id":       f["ItemId"],
		})
	}
	return map[string]any{"libraries": result}, nil
}

func (j *JFIntegration) itemDetails(params map[string]any) (any, error) {
	itemID, _ := params["item_id"].(string)
	if itemID == "" {
		return nil, fmt.Errorf("item_id required")
	}
	qp := url.Values{"Fields": {"Overview,People,Genres,CommunityRating,OfficialRating,RunTimeTicks,MediaStreams"}}
	data, err := j.apiGet("/Items/"+itemID, qp)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	var item map[string]any
	json.Unmarshal(data, &item)

	// Extract people (actors, directors)
	var actors, directors []string
	if people, ok := item["People"].([]any); ok {
		for _, p := range people {
			person, _ := p.(map[string]any)
			name, _ := person["Name"].(string)
			role, _ := person["Type"].(string)
			if role == "Director" {
				directors = append(directors, name)
			} else if role == "Actor" && len(actors) < 5 {
				actors = append(actors, name)
			}
		}
	}

	// Extract genres
	var genres []string
	if g, ok := item["Genres"].([]any); ok {
		for _, genre := range g {
			if s, ok := genre.(string); ok {
				genres = append(genres, s)
			}
		}
	}

	overview, _ := item["Overview"].(string)
	if len(overview) > 300 {
		overview = overview[:300] + "..."
	}

	// Runtime in minutes
	var runtimeMin int
	if ticks, ok := item["RunTimeTicks"].(float64); ok {
		runtimeMin = int(ticks / 10_000_000 / 60)
	}

	return map[string]any{
		"name":       item["Name"],
		"type":       item["Type"],
		"year":       item["ProductionYear"],
		"overview":   overview,
		"rating":     item["CommunityRating"],
		"mpaa":       item["OfficialRating"],
		"runtime":    fmt.Sprintf("%dmin", runtimeMin),
		"genres":     genres,
		"directors":  directors,
		"actors":     actors,
		"id":         item["Id"],
	}, nil
}

func (j *JFIntegration) play(params map[string]any) (any, error) {
	sessionID, _ := params["session_id"].(string)
	itemID, _ := params["item_id"].(string)
	if sessionID == "" || itemID == "" {
		return nil, fmt.Errorf("session_id and item_id required")
	}
	path := fmt.Sprintf("/Sessions/%s/Playing", sessionID)
	qp := url.Values{"ItemIds": {itemID}, "PlayCommand": {"PlayNow"}}
	_, err := j.apiPost(path, qp, nil)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return map[string]any{"status": "playing", "session_id": sessionID, "item_id": itemID}, nil
}

func (j *JFIntegration) playbackControl(params map[string]any) (any, error) {
	sessionID, _ := params["session_id"].(string)
	command, _ := params["command"].(string)
	if sessionID == "" || command == "" {
		return nil, fmt.Errorf("session_id and command required")
	}
	path := fmt.Sprintf("/Sessions/%s/Playing/%s", sessionID, command)
	_, err := j.apiPost(path, nil, nil)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return map[string]any{"status": command, "session_id": sessionID}, nil
}

func (j *JFIntegration) streamURL(params map[string]any) (any, error) {
	itemID, _ := params["item_id"].(string)
	if itemID == "" {
		return nil, fmt.Errorf("item_id required")
	}
	// Generate direct stream URL with API key
	streamURL := fmt.Sprintf("%s/Items/%s/Download?api_key=%s", j.url, itemID, j.apiKey)
	// Also provide a transcoded stream for wider compatibility
	transcodeURL := fmt.Sprintf("%s/Videos/%s/stream?Static=true&api_key=%s", j.url, itemID, j.apiKey)

	return map[string]any{
		"stream_url":    streamURL,
		"transcode_url": transcodeURL,
		"item_id":       itemID,
	}, nil
}

// ── HTTP Helpers ──────────────────────────────────────────────

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

func (j *JFIntegration) apiPost(path string, params url.Values, data []byte) ([]byte, error) {
	u := j.url + path
	if params == nil {
		params = url.Values{}
	}
	params.Set("api_key", j.apiKey)
	u += "?" + params.Encode()

	var bodyReader io.Reader
	if data != nil {
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequest("POST", u, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := j.client.Do(req)
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
