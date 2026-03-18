package qbittorrent

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/brooqs/steward/internal/integration"
	"github.com/brooqs/steward/internal/tools"
)

func init() {
	integration.Register("qbittorrent", func() integration.Integration {
		return &QBTIntegration{}
	})
}

// QBTIntegration integrates with qBittorrent's Web API.
type QBTIntegration struct {
	url      string
	username string
	password string
	enabled  bool
	client   *http.Client
	mu       sync.Mutex
	loggedIn bool
}

func (q *QBTIntegration) Name() string       { return "qbittorrent" }
func (q *QBTIntegration) Enabled() bool      { return q.enabled }
func (q *QBTIntegration) ToolPrefix() string  { return "qbt_" }

func (q *QBTIntegration) LoadConfig(cfg map[string]any) error {
	u, _ := cfg["url"].(string)
	if u == "" {
		return fmt.Errorf("qbittorrent requires 'url'")
	}
	q.url = strings.TrimRight(u, "/")
	q.username, _ = cfg["username"].(string)
	if q.username == "" {
		q.username = "admin"
	}
	q.password, _ = cfg["password"].(string)
	jar, _ := cookiejar.New(nil)
	q.client = &http.Client{Timeout: 10 * time.Second, Jar: jar}
	q.enabled = true
	return nil
}

func (q *QBTIntegration) HealthCheck() bool {
	if !q.enabled {
		return false
	}
	return q.ensureLogin() == nil
}

func (q *QBTIntegration) GetTools() []tools.ToolSpec {
	if !q.enabled {
		return nil
	}
	return []tools.ToolSpec{
		{
			Name:        "qbt_list_torrents",
			Description: "List torrents in qBittorrent. Filter by status.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"filter": map[string]any{"type": "string", "description": "all | active | completed | paused (default: all)"},
				},
				"required": []string{},
			},
			Handler: q.listTorrents,
		},
		{
			Name:        "qbt_add_torrent",
			Description: "Add a torrent by magnet link or .torrent URL.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url":       map[string]any{"type": "string", "description": "Magnet link or torrent URL"},
					"save_path": map[string]any{"type": "string", "description": "Optional save directory"},
				},
				"required": []string{"url"},
			},
			Handler: q.addTorrent,
		},
		{
			Name:        "qbt_pause_torrent",
			Description: "Pause a torrent by its hash.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"torrent_hash": map[string]any{"type": "string", "description": "Torrent hash"},
				},
				"required": []string{"torrent_hash"},
			},
			Handler: q.pauseTorrent,
		},
		{
			Name:        "qbt_resume_torrent",
			Description: "Resume a paused torrent by its hash.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"torrent_hash": map[string]any{"type": "string", "description": "Torrent hash"},
				},
				"required": []string{"torrent_hash"},
			},
			Handler: q.resumeTorrent,
		},
	}
}

func (q *QBTIntegration) ensureLogin() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.loggedIn {
		return nil
	}
	form := url.Values{"username": {q.username}, "password": {q.password}}
	resp, err := q.client.PostForm(q.url+"/api/v2/auth/login", form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "Ok." {
		return fmt.Errorf("login failed: %s", string(body))
	}
	q.loggedIn = true
	return nil
}

func (q *QBTIntegration) listTorrents(params map[string]any) (any, error) {
	if err := q.ensureLogin(); err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	filter := "all"
	if f, ok := params["filter"].(string); ok && f != "" {
		filter = f
	}
	resp, err := q.client.Get(q.url + "/api/v2/torrents/info?filter=" + filter)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var torrents []map[string]any
	json.Unmarshal(body, &torrents)

	var result []map[string]any
	for i, t := range torrents {
		if i >= 20 {
			break
		}
		progress, _ := t["progress"].(float64)
		size, _ := t["size"].(float64)
		result = append(result, map[string]any{
			"name":        t["name"],
			"state":       t["state"],
			"progress":    fmt.Sprintf("%.1f%%", progress*100),
			"size_gb":     fmt.Sprintf("%.2f", size/1e9),
			"eta_seconds": t["eta"],
			"hash":        t["hash"],
		})
	}
	return result, nil
}

func (q *QBTIntegration) addTorrent(params map[string]any) (any, error) {
	if err := q.ensureLogin(); err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	torrentURL, _ := params["url"].(string)
	if torrentURL == "" {
		return nil, fmt.Errorf("url required")
	}
	form := url.Values{"urls": {torrentURL}}
	if sp, ok := params["save_path"].(string); ok && sp != "" {
		form.Set("savepath", sp)
	}
	resp, err := q.client.PostForm(q.url+"/api/v2/torrents/add", form)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return map[string]any{"success": string(body) == "Ok.", "message": string(body)}, nil
}

func (q *QBTIntegration) pauseTorrent(params map[string]any) (any, error) {
	if err := q.ensureLogin(); err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	hash, _ := params["torrent_hash"].(string)
	form := url.Values{"hashes": {hash}}
	_, err := q.client.PostForm(q.url+"/api/v2/torrents/pause", form)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return map[string]any{"success": true}, nil
}

func (q *QBTIntegration) resumeTorrent(params map[string]any) (any, error) {
	if err := q.ensureLogin(); err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	hash, _ := params["torrent_hash"].(string)
	form := url.Values{"hashes": {hash}}
	_, err := q.client.PostForm(q.url+"/api/v2/torrents/resume", form)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return map[string]any{"success": true}, nil
}
