package gmail

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/brooqs/steward/internal/integration"
	"github.com/brooqs/steward/internal/tools"
)

func init() {
	integration.Register("gmail", func() integration.Integration {
		return &GmailIntegration{}
	})
}

// GmailIntegration integrates with Gmail API via OAuth2.
type GmailIntegration struct {
	clientID     string
	clientSecret string
	refreshToken string
	accessToken  string
	tokenExpiry  time.Time
	enabled      bool
	client       *http.Client
	mu           sync.Mutex
}

func (g *GmailIntegration) Name() string       { return "gmail" }
func (g *GmailIntegration) Enabled() bool      { return g.enabled }
func (g *GmailIntegration) ToolPrefix() string { return "gmail_" }

func (g *GmailIntegration) LoadConfig(cfg map[string]any) error {
	g.clientID, _ = cfg["client_id"].(string)
	g.clientSecret, _ = cfg["client_secret"].(string)
	g.refreshToken, _ = cfg["refresh_token"].(string)

	if g.clientID == "" || g.clientSecret == "" || g.refreshToken == "" {
		return fmt.Errorf("gmail requires 'client_id', 'client_secret', and 'refresh_token'")
	}

	g.enabled = true
	g.client = &http.Client{Timeout: 15 * time.Second}

	if err := g.refreshAccessToken(); err != nil {
		return fmt.Errorf("initial token refresh: %w", err)
	}

	slog.Info("gmail connected", "token_expiry", g.tokenExpiry.Format(time.RFC3339))
	return nil
}

func (g *GmailIntegration) HealthCheck() bool {
	if !g.enabled {
		return false
	}
	_, err := g.apiGet("/gmail/v1/users/me/profile")
	return err == nil
}

func (g *GmailIntegration) GetTools() []tools.ToolSpec {
	if !g.enabled {
		return nil
	}
	return []tools.ToolSpec{
		{
			Name:        "gmail_inbox",
			Description: "Get recent emails from the inbox",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit": map[string]any{"type": "integer", "description": "Number of emails (default: 10, max: 20)"},
				},
			},
			Handler: g.inbox,
		},
		{
			Name:        "gmail_read",
			Description: "Read the full content of a specific email",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"message_id": map[string]any{"type": "string", "description": "Message ID (from gmail_inbox or gmail_search)"},
				},
				"required": []string{"message_id"},
			},
			Handler: g.readMessage,
		},
		{
			Name:        "gmail_send",
			Description: "Send an email",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"to":      map[string]any{"type": "string", "description": "Recipient email address"},
					"subject": map[string]any{"type": "string", "description": "Email subject"},
					"body":    map[string]any{"type": "string", "description": "Email body (plain text)"},
				},
				"required": []string{"to", "subject", "body"},
			},
			Handler: g.send,
		},
		{
			Name:        "gmail_search",
			Description: "Search emails using Gmail query syntax",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "Gmail search query (e.g. 'from:user@example.com', 'subject:invoice', 'is:unread')"},
					"limit": map[string]any{"type": "integer", "description": "Number of results (default: 10, max: 20)"},
				},
				"required": []string{"query"},
			},
			Handler: g.search,
		},
		{
			Name:        "gmail_labels",
			Description: "List all Gmail labels/folders",
			Parameters: map[string]any{"type": "object", "properties": map[string]any{}},
			Handler:     g.labels,
		},
		{
			Name:        "gmail_mark_read",
			Description: "Mark an email as read",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"message_id": map[string]any{"type": "string", "description": "Message ID to mark as read"},
				},
				"required": []string{"message_id"},
			},
			Handler: g.markRead,
		},
	}
}

// ── Tool Handlers ─────────────────────────────────────────────

func (g *GmailIntegration) inbox(params map[string]any) (any, error) {
	limit := 10
	if l, ok := params["limit"].(float64); ok && l > 0 {
		limit = int(l)
		if limit > 20 {
			limit = 20
		}
	}
	return g.listMessages("in:inbox", limit)
}

func (g *GmailIntegration) search(params map[string]any) (any, error) {
	query, _ := params["query"].(string)
	if query == "" {
		return nil, fmt.Errorf("query required")
	}
	limit := 10
	if l, ok := params["limit"].(float64); ok && l > 0 {
		limit = int(l)
		if limit > 20 {
			limit = 20
		}
	}
	return g.listMessages(query, limit)
}

func (g *GmailIntegration) listMessages(query string, limit int) (any, error) {
	path := fmt.Sprintf("/gmail/v1/users/me/messages?q=%s&maxResults=%d",
		url.QueryEscape(query), limit)
	data, err := g.apiGet(path)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	var resp map[string]any
	json.Unmarshal(data, &resp)

	rawMsgs, _ := resp["messages"].([]any)
	if len(rawMsgs) == 0 {
		return map[string]any{"messages": []any{}, "total": 0}, nil
	}

	// Fetch headers for each message
	var messages []map[string]any
	for _, raw := range rawMsgs {
		msg, _ := raw.(map[string]any)
		msgID, _ := msg["id"].(string)

		detail, err := g.apiGet(fmt.Sprintf("/gmail/v1/users/me/messages/%s?format=metadata&metadataHeaders=From&metadataHeaders=Subject&metadataHeaders=Date", msgID))
		if err != nil {
			continue
		}

		var msgDetail map[string]any
		json.Unmarshal(detail, &msgDetail)

		entry := map[string]any{"id": msgID, "snippet": msgDetail["snippet"]}

		// Extract headers
		if payload, ok := msgDetail["payload"].(map[string]any); ok {
			if headers, ok := payload["headers"].([]any); ok {
				for _, h := range headers {
					hdr, _ := h.(map[string]any)
					name, _ := hdr["name"].(string)
					value, _ := hdr["value"].(string)
					switch strings.ToLower(name) {
					case "from":
						entry["from"] = value
					case "subject":
						entry["subject"] = value
					case "date":
						entry["date"] = value
					}
				}
			}
		}

		// Check if unread
		if labels, ok := msgDetail["labelIds"].([]any); ok {
			for _, l := range labels {
				if l == "UNREAD" {
					entry["unread"] = true
					break
				}
			}
		}

		messages = append(messages, entry)
	}

	return map[string]any{"messages": messages, "count": len(messages)}, nil
}

func (g *GmailIntegration) readMessage(params map[string]any) (any, error) {
	msgID, _ := params["message_id"].(string)
	if msgID == "" {
		return nil, fmt.Errorf("message_id required")
	}

	data, err := g.apiGet(fmt.Sprintf("/gmail/v1/users/me/messages/%s?format=full", msgID))
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	var msg map[string]any
	json.Unmarshal(data, &msg)

	result := map[string]any{"id": msgID, "snippet": msg["snippet"]}

	// Extract headers
	if payload, ok := msg["payload"].(map[string]any); ok {
		if headers, ok := payload["headers"].([]any); ok {
			for _, h := range headers {
				hdr, _ := h.(map[string]any)
				name, _ := hdr["name"].(string)
				value, _ := hdr["value"].(string)
				switch strings.ToLower(name) {
				case "from":
					result["from"] = value
				case "to":
					result["to"] = value
				case "subject":
					result["subject"] = value
				case "date":
					result["date"] = value
				}
			}
		}

		// Extract body
		body := extractBody(payload)
		if len(body) > 2000 {
			body = body[:2000] + "...(truncated)"
		}
		result["body"] = body
	}

	return result, nil
}

func (g *GmailIntegration) send(params map[string]any) (any, error) {
	to, _ := params["to"].(string)
	subject, _ := params["subject"].(string)
	body, _ := params["body"].(string)

	if to == "" || subject == "" || body == "" {
		return nil, fmt.Errorf("to, subject, and body required")
	}

	// Build RFC 2822 message
	raw := fmt.Sprintf("To: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		to, subject, body)

	encoded := base64.URLEncoding.EncodeToString([]byte(raw))
	payload, _ := json.Marshal(map[string]string{"raw": encoded})

	data, err := g.apiPost("/gmail/v1/users/me/messages/send", payload)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	var resp map[string]any
	json.Unmarshal(data, &resp)

	return map[string]any{"status": "sent", "id": resp["id"], "to": to, "subject": subject}, nil
}

func (g *GmailIntegration) labels(params map[string]any) (any, error) {
	data, err := g.apiGet("/gmail/v1/users/me/labels")
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	var resp map[string]any
	json.Unmarshal(data, &resp)

	rawLabels, _ := resp["labels"].([]any)
	var result []map[string]any
	for _, raw := range rawLabels {
		label, _ := raw.(map[string]any)
		result = append(result, map[string]any{
			"id":   label["id"],
			"name": label["name"],
			"type": label["type"],
		})
	}
	return map[string]any{"labels": result}, nil
}

func (g *GmailIntegration) markRead(params map[string]any) (any, error) {
	msgID, _ := params["message_id"].(string)
	if msgID == "" {
		return nil, fmt.Errorf("message_id required")
	}

	payload, _ := json.Marshal(map[string]any{
		"removeLabelIds": []string{"UNREAD"},
	})

	path := fmt.Sprintf("/gmail/v1/users/me/messages/%s/modify", msgID)
	_, err := g.apiPost(path, payload)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return map[string]any{"status": "marked_read", "message_id": msgID}, nil
}

// ── OAuth2 Token Management ───────────────────────────────────

func (g *GmailIntegration) refreshAccessToken() error {
	data := url.Values{
		"client_id":     {g.clientID},
		"client_secret": {g.clientSecret},
		"refresh_token": {g.refreshToken},
		"grant_type":    {"refresh_token"},
	}

	resp, err := g.client.PostForm("https://oauth2.googleapis.com/token", data)
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

	g.mu.Lock()
	g.accessToken = tokenResp.AccessToken
	g.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	g.mu.Unlock()

	return nil
}

func (g *GmailIntegration) getToken() (string, error) {
	g.mu.Lock()
	expired := time.Now().After(g.tokenExpiry.Add(-60 * time.Second))
	g.mu.Unlock()

	if expired {
		if err := g.refreshAccessToken(); err != nil {
			return "", err
		}
	}
	g.mu.Lock()
	token := g.accessToken
	g.mu.Unlock()
	return token, nil
}

// ── HTTP Helpers ──────────────────────────────────────────────

func (g *GmailIntegration) apiGet(path string) ([]byte, error) {
	token, err := g.getToken()
	if err != nil {
		return nil, err
	}
	req, _ := http.NewRequest("GET", "https://www.googleapis.com"+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := g.client.Do(req)
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

func (g *GmailIntegration) apiPost(path string, data []byte) ([]byte, error) {
	token, err := g.getToken()
	if err != nil {
		return nil, err
	}
	var bodyReader io.Reader
	if data != nil {
		bodyReader = bytes.NewReader(data)
	}
	req, _ := http.NewRequest("POST", "https://www.googleapis.com"+path, bodyReader)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
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

// ── Helpers ───────────────────────────────────────────────────

// extractBody recursively extracts text/plain body from message payload.
func extractBody(payload map[string]any) string {
	// Check for direct body data
	if body, ok := payload["body"].(map[string]any); ok {
		if data, ok := body["data"].(string); ok && data != "" {
			mimeType, _ := payload["mimeType"].(string)
			if strings.HasPrefix(mimeType, "text/plain") || mimeType == "" {
				decoded, err := base64.URLEncoding.DecodeString(data)
				if err == nil {
					return string(decoded)
				}
			}
		}
	}

	// Check parts (multipart messages)
	if parts, ok := payload["parts"].([]any); ok {
		// First try text/plain
		for _, part := range parts {
			p, _ := part.(map[string]any)
			mimeType, _ := p["mimeType"].(string)
			if mimeType == "text/plain" {
				return extractBody(p)
			}
		}
		// Fallback to first text part
		for _, part := range parts {
			p, _ := part.(map[string]any)
			mimeType, _ := p["mimeType"].(string)
			if strings.HasPrefix(mimeType, "text/") {
				return extractBody(p)
			}
			// Recurse into multipart
			if strings.HasPrefix(mimeType, "multipart/") {
				result := extractBody(p)
				if result != "" {
					return result
				}
			}
		}
	}

	return ""
}

// decodeHeader decodes RFC 2047 encoded header values.
func decodeHeader(s string) string {
	dec := new(mime.WordDecoder)
	result, err := dec.DecodeHeader(s)
	if err != nil {
		return s
	}
	return result
}
