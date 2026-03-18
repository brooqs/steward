package spotify

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/brooqs/steward/internal/integration"
	"github.com/brooqs/steward/internal/tools"
)

func init() {
	integration.Register("spotify", func() integration.Integration {
		return &SpotifyIntegration{}
	})
}

// SpotifyIntegration integrates with Spotify Web API.
type SpotifyIntegration struct {
	clientID     string
	clientSecret string
	refreshToken string
	accessToken  string
	tokenExpiry  time.Time
	enabled      bool
	client       *http.Client
	mu           sync.Mutex
}

func (s *SpotifyIntegration) Name() string      { return "spotify" }
func (s *SpotifyIntegration) Enabled() bool     { return s.enabled }
func (s *SpotifyIntegration) ToolPrefix() string { return "spotify_" }

func (s *SpotifyIntegration) LoadConfig(cfg map[string]any) error {
	s.clientID, _ = cfg["client_id"].(string)
	s.clientSecret, _ = cfg["client_secret"].(string)
	s.refreshToken, _ = cfg["refresh_token"].(string)

	if s.clientID == "" || s.clientSecret == "" || s.refreshToken == "" {
		return fmt.Errorf("spotify requires 'client_id', 'client_secret', and 'refresh_token'")
	}

	s.enabled = true
	s.client = &http.Client{Timeout: 10 * time.Second}

	// Get initial access token
	if err := s.refreshAccessToken(); err != nil {
		return fmt.Errorf("initial token refresh: %w", err)
	}

	slog.Info("spotify connected", "token_expiry", s.tokenExpiry.Format(time.RFC3339))
	return nil
}

func (s *SpotifyIntegration) HealthCheck() bool {
	if !s.enabled {
		return false
	}
	_, err := s.apiGet("/v1/me")
	return err == nil
}

func (s *SpotifyIntegration) GetTools() []tools.ToolSpec {
	if !s.enabled {
		return nil
	}
	return []tools.ToolSpec{
		{
			Name:        "spotify_now_playing",
			Description: "Get the currently playing track on Spotify",
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
			Handler:     s.nowPlaying,
		},
		{
			Name:        "spotify_play",
			Description: "Resume playback or play a specific track/album/playlist on Spotify",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"uri": map[string]any{
						"type":        "string",
						"description": "Spotify URI to play (e.g. spotify:track:xxx). Leave empty to resume current playback.",
					},
				},
			},
			Handler: s.play,
		},
		{
			Name:        "spotify_pause",
			Description: "Pause Spotify playback",
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
			Handler:     s.pause,
		},
		{
			Name:        "spotify_next",
			Description: "Skip to next track on Spotify",
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
			Handler:     s.next,
		},
		{
			Name:        "spotify_previous",
			Description: "Go to previous track on Spotify",
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
			Handler:     s.previous,
		},
		{
			Name:        "spotify_search",
			Description: "Search for tracks, albums, artists, or playlists on Spotify",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "Search query"},
					"type": map[string]any{
						"type":        "string",
						"description": "Type to search: track, album, artist, playlist (default: track)",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Number of results (default: 5, max: 10)",
					},
				},
				"required": []string{"query"},
			},
			Handler: s.search,
		},
		{
			Name:        "spotify_volume",
			Description: "Set Spotify playback volume (0-100)",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"volume": map[string]any{"type": "integer", "description": "Volume level 0-100"},
				},
				"required": []string{"volume"},
			},
			Handler: s.setVolume,
		},
		{
			Name:        "spotify_queue",
			Description: "Add a track to the Spotify playback queue",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"uri": map[string]any{"type": "string", "description": "Spotify URI of the track to add"},
				},
				"required": []string{"uri"},
			},
			Handler: s.addToQueue,
		},
		{
			Name:        "spotify_devices",
			Description: "List available Spotify playback devices",
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
			Handler:     s.listDevices,
		},
		{
			Name:        "spotify_my_playlists",
			Description: "List the user's own Spotify playlists",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit": map[string]any{
						"type":        "integer",
						"description": "Number of playlists to return (default: 10, max: 20)",
					},
				},
			},
			Handler: s.myPlaylists,
		},
		{
			Name:        "spotify_shuffle",
			Description: "Toggle shuffle mode on/off for Spotify playback",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"state": map[string]any{
						"type":        "boolean",
						"description": "true to enable shuffle, false to disable",
					},
				},
				"required": []string{"state"},
			},
			Handler: s.shuffle,
		},
		{
			Name:        "spotify_repeat",
			Description: "Set repeat mode for Spotify playback",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"state": map[string]any{
						"type":        "string",
						"description": "Repeat mode: 'track' (repeat current), 'context' (repeat playlist/album), 'off' (no repeat)",
						"enum":        []string{"track", "context", "off"},
					},
				},
				"required": []string{"state"},
			},
			Handler: s.repeat,
		},
		{
			Name:        "spotify_transfer",
			Description: "Transfer Spotify playback to a different device",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"device_id": map[string]any{
						"type":        "string",
						"description": "ID of the target device (use spotify_devices to find device IDs)",
					},
					"play": map[string]any{
						"type":        "boolean",
						"description": "Whether to start playing on the new device (default: true)",
					},
				},
				"required": []string{"device_id"},
			},
			Handler: s.transferPlayback,
		},
		{
			Name:        "spotify_recently_played",
			Description: "Get the user's recently played tracks on Spotify",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit": map[string]any{
						"type":        "integer",
						"description": "Number of tracks to return (default: 10, max: 20)",
					},
				},
			},
			Handler: s.recentlyPlayed,
		},
	}
}

// ── Tool Handlers ─────────────────────────────────────────────

func (s *SpotifyIntegration) nowPlaying(params map[string]any) (any, error) {
	data, err := s.apiGet("/v1/me/player/currently-playing")
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	if len(data) == 0 {
		return map[string]any{"status": "nothing_playing"}, nil
	}

	var result map[string]any
	json.Unmarshal(data, &result)

	item, _ := result["item"].(map[string]any)
	if item == nil {
		return map[string]any{"status": "nothing_playing"}, nil
	}

	// Extract artist names
	artists := extractArtists(item)
	album, _ := item["album"].(map[string]any)
	albumName, _ := album["name"].(string)
	progressMs, _ := result["progress_ms"].(float64)
	durationMs, _ := item["duration_ms"].(float64)
	isPlaying, _ := result["is_playing"].(bool)

	return map[string]any{
		"track":      item["name"],
		"artists":    artists,
		"album":      albumName,
		"uri":        item["uri"],
		"is_playing": isPlaying,
		"progress":   formatDuration(int(progressMs)),
		"duration":   formatDuration(int(durationMs)),
	}, nil
}

func (s *SpotifyIntegration) play(params map[string]any) (any, error) {
	uri, _ := params["uri"].(string)

	if uri == "" {
		// Resume playback
		_, err := s.apiPut("/v1/me/player/play", nil)
		if err != nil {
			return map[string]any{"error": err.Error()}, nil
		}
		return map[string]any{"status": "resumed"}, nil
	}

	// Play specific URI
	var body map[string]any
	if strings.Contains(uri, ":track:") {
		body = map[string]any{"uris": []string{uri}}
	} else {
		body = map[string]any{"context_uri": uri}
	}
	data, _ := json.Marshal(body)
	_, err := s.apiPut("/v1/me/player/play", data)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return map[string]any{"status": "playing", "uri": uri}, nil
}

func (s *SpotifyIntegration) pause(params map[string]any) (any, error) {
	_, err := s.apiPut("/v1/me/player/pause", nil)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return map[string]any{"status": "paused"}, nil
}

func (s *SpotifyIntegration) next(params map[string]any) (any, error) {
	_, err := s.apiPost("/v1/me/player/next", nil)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return map[string]any{"status": "skipped_to_next"}, nil
}

func (s *SpotifyIntegration) previous(params map[string]any) (any, error) {
	_, err := s.apiPost("/v1/me/player/previous", nil)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return map[string]any{"status": "skipped_to_previous"}, nil
}

func (s *SpotifyIntegration) search(params map[string]any) (any, error) {
	query, _ := params["query"].(string)
	if query == "" {
		return nil, fmt.Errorf("query required")
	}

	searchType, _ := params["type"].(string)
	if searchType == "" {
		searchType = "track"
	}

	limit := 5
	if l, ok := params["limit"].(float64); ok && l > 0 {
		limit = int(l)
		if limit > 10 {
			limit = 10
		}
	}

	path := fmt.Sprintf("/v1/search?q=%s&type=%s&limit=%d",
		url.QueryEscape(query), searchType, limit)

	data, err := s.apiGet(path)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	var result map[string]any
	json.Unmarshal(data, &result)

	// Parse results based on type
	var items []map[string]any
	switch searchType {
	case "track":
		tracks, _ := result["tracks"].(map[string]any)
		rawItems, _ := tracks["items"].([]any)
		for _, raw := range rawItems {
			item, _ := raw.(map[string]any)
			album, _ := item["album"].(map[string]any)
			albumName, _ := album["name"].(string)
			items = append(items, map[string]any{
				"name":    item["name"],
				"artists": extractArtists(item),
				"album":   albumName,
				"uri":     item["uri"],
			})
		}
	case "artist":
		artists, _ := result["artists"].(map[string]any)
		rawItems, _ := artists["items"].([]any)
		for _, raw := range rawItems {
			item, _ := raw.(map[string]any)
			followers, _ := item["followers"].(map[string]any)
			items = append(items, map[string]any{
				"name":      item["name"],
				"uri":       item["uri"],
				"followers": followers["total"],
			})
		}
	case "album":
		albums, _ := result["albums"].(map[string]any)
		rawItems, _ := albums["items"].([]any)
		for _, raw := range rawItems {
			item, _ := raw.(map[string]any)
			items = append(items, map[string]any{
				"name":    item["name"],
				"artists": extractArtists(item),
				"uri":     item["uri"],
			})
		}
	case "playlist":
		playlists, _ := result["playlists"].(map[string]any)
		rawItems, _ := playlists["items"].([]any)
		for _, raw := range rawItems {
			item, _ := raw.(map[string]any)
			owner, _ := item["owner"].(map[string]any)
			items = append(items, map[string]any{
				"name":  item["name"],
				"owner": owner["display_name"],
				"uri":   item["uri"],
			})
		}
	}

	return map[string]any{"results": items, "type": searchType, "query": query}, nil
}

func (s *SpotifyIntegration) setVolume(params map[string]any) (any, error) {
	vol, _ := params["volume"].(float64)
	if vol < 0 {
		vol = 0
	}
	if vol > 100 {
		vol = 100
	}
	path := fmt.Sprintf("/v1/me/player/volume?volume_percent=%d", int(vol))
	_, err := s.apiPut(path, nil)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return map[string]any{"volume": int(vol)}, nil
}

func (s *SpotifyIntegration) addToQueue(params map[string]any) (any, error) {
	uri, _ := params["uri"].(string)
	if uri == "" {
		return nil, fmt.Errorf("uri required")
	}
	path := "/v1/me/player/queue?uri=" + url.QueryEscape(uri)
	_, err := s.apiPost(path, nil)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return map[string]any{"status": "added_to_queue", "uri": uri}, nil
}

func (s *SpotifyIntegration) listDevices(params map[string]any) (any, error) {
	data, err := s.apiGet("/v1/me/player/devices")
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	var result map[string]any
	json.Unmarshal(data, &result)

	rawDevices, _ := result["devices"].([]any)
	var devices []map[string]any
	for _, raw := range rawDevices {
		dev, _ := raw.(map[string]any)
		devices = append(devices, map[string]any{
			"name":      dev["name"],
			"type":      dev["type"],
			"id":        dev["id"],
			"is_active": dev["is_active"],
			"volume":    dev["volume_percent"],
		})
	}
	return map[string]any{"devices": devices}, nil
}

func (s *SpotifyIntegration) myPlaylists(params map[string]any) (any, error) {
	limit := 10
	if l, ok := params["limit"].(float64); ok && l > 0 {
		limit = int(l)
		if limit > 20 {
			limit = 20
		}
	}

	path := fmt.Sprintf("/v1/me/playlists?limit=%d", limit)
	data, err := s.apiGet(path)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	var result map[string]any
	json.Unmarshal(data, &result)

	rawItems, _ := result["items"].([]any)
	var playlists []map[string]any
	for _, raw := range rawItems {
		item, _ := raw.(map[string]any)
		owner, _ := item["owner"].(map[string]any)
		tracks, _ := item["tracks"].(map[string]any)
		playlists = append(playlists, map[string]any{
			"name":        item["name"],
			"uri":         item["uri"],
			"owner":       owner["display_name"],
			"track_count": tracks["total"],
			"public":      item["public"],
		})
	}
	return map[string]any{"playlists": playlists, "total": result["total"]}, nil
}

func (s *SpotifyIntegration) shuffle(params map[string]any) (any, error) {
	state, _ := params["state"].(bool)
	stateStr := "false"
	if state {
		stateStr = "true"
	}
	path := "/v1/me/player/shuffle?state=" + stateStr
	_, err := s.apiPut(path, nil)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return map[string]any{"shuffle": state}, nil
}

func (s *SpotifyIntegration) repeat(params map[string]any) (any, error) {
	state, _ := params["state"].(string)
	if state != "track" && state != "context" && state != "off" {
		return nil, fmt.Errorf("state must be 'track', 'context', or 'off'")
	}
	path := "/v1/me/player/repeat?state=" + state
	_, err := s.apiPut(path, nil)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return map[string]any{"repeat": state}, nil
}

func (s *SpotifyIntegration) transferPlayback(params map[string]any) (any, error) {
	deviceID, _ := params["device_id"].(string)
	if deviceID == "" {
		return nil, fmt.Errorf("device_id required")
	}
	play := true
	if p, ok := params["play"].(bool); ok {
		play = p
	}

	body, _ := json.Marshal(map[string]any{
		"device_ids": []string{deviceID},
		"play":       play,
	})
	_, err := s.apiPut("/v1/me/player", body)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return map[string]any{"status": "transferred", "device_id": deviceID, "play": play}, nil
}

func (s *SpotifyIntegration) recentlyPlayed(params map[string]any) (any, error) {
	limit := 10
	if l, ok := params["limit"].(float64); ok && l > 0 {
		limit = int(l)
		if limit > 20 {
			limit = 20
		}
	}

	path := fmt.Sprintf("/v1/me/player/recently-played?limit=%d", limit)
	data, err := s.apiGet(path)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	var result map[string]any
	json.Unmarshal(data, &result)

	rawItems, _ := result["items"].([]any)
	var tracks []map[string]any
	for _, raw := range rawItems {
		item, _ := raw.(map[string]any)
		track, _ := item["track"].(map[string]any)
		if track == nil {
			continue
		}
		album, _ := track["album"].(map[string]any)
		albumName, _ := album["name"].(string)
		playedAt, _ := item["played_at"].(string)
		tracks = append(tracks, map[string]any{
			"track":     track["name"],
			"artists":   extractArtists(track),
			"album":     albumName,
			"uri":       track["uri"],
			"played_at": playedAt,
		})
	}
	return map[string]any{"recently_played": tracks}, nil
}

// ── OAuth2 Token Management ───────────────────────────────────

func (s *SpotifyIntegration) refreshAccessToken() error {
	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {s.refreshToken},
	}

	req, _ := http.NewRequest("POST", "https://accounts.spotify.com/api/token",
		strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Basic "+
		base64.StdEncoding.EncodeToString([]byte(s.clientID+":"+s.clientSecret)))

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("token refresh failed: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	json.Unmarshal(body, &tokenResp)

	s.mu.Lock()
	s.accessToken = tokenResp.AccessToken
	s.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	s.mu.Unlock()

	return nil
}

func (s *SpotifyIntegration) getToken() (string, error) {
	s.mu.Lock()
	expired := time.Now().After(s.tokenExpiry.Add(-60 * time.Second)) // refresh 1 min early
	s.mu.Unlock()

	if expired {
		if err := s.refreshAccessToken(); err != nil {
			return "", err
		}
	}
	s.mu.Lock()
	token := s.accessToken
	s.mu.Unlock()
	return token, nil
}

// ── HTTP Helpers ──────────────────────────────────────────────

func (s *SpotifyIntegration) apiGet(path string) ([]byte, error) {
	token, err := s.getToken()
	if err != nil {
		return nil, err
	}
	req, _ := http.NewRequest("GET", "https://api.spotify.com"+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 204 {
		return nil, nil
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func (s *SpotifyIntegration) apiPut(path string, data []byte) ([]byte, error) {
	token, err := s.getToken()
	if err != nil {
		return nil, err
	}
	var bodyReader io.Reader
	if data != nil {
		bodyReader = bytes.NewReader(data)
	}
	req, _ := http.NewRequest("PUT", "https://api.spotify.com"+path, bodyReader)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 204 {
		return nil, nil
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func (s *SpotifyIntegration) apiPost(path string, data []byte) ([]byte, error) {
	token, err := s.getToken()
	if err != nil {
		return nil, err
	}
	var bodyReader io.Reader
	if data != nil {
		bodyReader = bytes.NewReader(data)
	}
	req, _ := http.NewRequest("POST", "https://api.spotify.com"+path, bodyReader)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 204 {
		return nil, nil
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

// ── Helpers ───────────────────────────────────────────────────

func extractArtists(item map[string]any) string {
	artists, _ := item["artists"].([]any)
	var names []string
	for _, a := range artists {
		artist, _ := a.(map[string]any)
		name, _ := artist["name"].(string)
		if name != "" {
			names = append(names, name)
		}
	}
	return strings.Join(names, ", ")
}

func formatDuration(ms int) string {
	s := ms / 1000
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}
