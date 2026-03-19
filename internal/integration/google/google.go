package google

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
	integration.Register("google", func() integration.Integration {
		return &GoogleIntegration{}
	})
}

// GoogleIntegration provides Gmail, Google Calendar, and Google Drive tools.
type GoogleIntegration struct {
	clientID     string
	clientSecret string
	refreshToken string
	accessToken  string
	tokenExpiry  time.Time
	enabled      bool
	client       *http.Client
	mu           sync.Mutex
}

func (g *GoogleIntegration) Name() string       { return "google" }
func (g *GoogleIntegration) Enabled() bool      { return g.enabled }
func (g *GoogleIntegration) ToolPrefix() string { return "g" } // gmail_, gcal_, gdrive_

func (g *GoogleIntegration) LoadConfig(cfg map[string]any) error {
	g.clientID, _ = cfg["client_id"].(string)
	g.clientSecret, _ = cfg["client_secret"].(string)
	g.refreshToken, _ = cfg["refresh_token"].(string)

	if g.clientID == "" || g.clientSecret == "" || g.refreshToken == "" {
		return fmt.Errorf("google requires 'client_id', 'client_secret', and 'refresh_token'")
	}

	g.enabled = true
	g.client = &http.Client{Timeout: 15 * time.Second}

	if err := g.refreshAccessToken(); err != nil {
		return fmt.Errorf("initial token refresh: %w", err)
	}

	slog.Info("google connected", "token_expiry", g.tokenExpiry.Format(time.RFC3339))
	return nil
}

func (g *GoogleIntegration) HealthCheck() bool {
	if !g.enabled {
		return false
	}
	// Quick check via Gmail profile
	_, err := g.apiGet("https://www.googleapis.com/gmail/v1/users/me/profile")
	return err == nil
}

func (g *GoogleIntegration) GetTools() []tools.ToolSpec {
	if !g.enabled {
		return nil
	}
	return append(append(g.gmailTools(), g.calendarTools()...), g.driveTools()...)
}

// ══════════════════════════════════════════════════════════════
// ── GMAIL TOOLS ──────────────────────────────────────────────
// ══════════════════════════════════════════════════════════════

func (g *GoogleIntegration) gmailTools() []tools.ToolSpec {
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
			Handler: g.gmailInbox,
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
			Handler: g.gmailRead,
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
			Handler: g.gmailSend,
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
			Handler: g.gmailSearch,
		},
		{
			Name:        "gmail_labels",
			Description: "List all Gmail labels/folders",
			Parameters: map[string]any{"type": "object", "properties": map[string]any{}},
			Handler:     g.gmailLabels,
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
			Handler: g.gmailMarkRead,
		},
	}
}

func (g *GoogleIntegration) gmailInbox(params map[string]any) (any, error) {
	limit := 10
	if l, ok := params["limit"].(float64); ok && l > 0 {
		limit = int(l)
		if limit > 20 {
			limit = 20
		}
	}
	return g.gmailListMessages("in:inbox", limit)
}

func (g *GoogleIntegration) gmailSearch(params map[string]any) (any, error) {
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
	return g.gmailListMessages(query, limit)
}

func (g *GoogleIntegration) gmailListMessages(query string, limit int) (any, error) {
	path := fmt.Sprintf("https://www.googleapis.com/gmail/v1/users/me/messages?q=%s&maxResults=%d",
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

	var messages []map[string]any
	for _, raw := range rawMsgs {
		msg, _ := raw.(map[string]any)
		msgID, _ := msg["id"].(string)

		detail, err := g.apiGet(fmt.Sprintf("https://www.googleapis.com/gmail/v1/users/me/messages/%s?format=metadata&metadataHeaders=From&metadataHeaders=Subject&metadataHeaders=Date", msgID))
		if err != nil {
			continue
		}

		var msgDetail map[string]any
		json.Unmarshal(detail, &msgDetail)

		entry := map[string]any{"id": msgID, "snippet": msgDetail["snippet"]}
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

func (g *GoogleIntegration) gmailRead(params map[string]any) (any, error) {
	msgID, _ := params["message_id"].(string)
	if msgID == "" {
		return nil, fmt.Errorf("message_id required")
	}

	data, err := g.apiGet(fmt.Sprintf("https://www.googleapis.com/gmail/v1/users/me/messages/%s?format=full", msgID))
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	var msg map[string]any
	json.Unmarshal(data, &msg)
	result := map[string]any{"id": msgID, "snippet": msg["snippet"]}

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
		body := extractBody(payload)
		if len(body) > 2000 {
			body = body[:2000] + "...(truncated)"
		}
		result["body"] = body
	}
	return result, nil
}

func (g *GoogleIntegration) gmailSend(params map[string]any) (any, error) {
	to, _ := params["to"].(string)
	subject, _ := params["subject"].(string)
	body, _ := params["body"].(string)
	if to == "" || subject == "" || body == "" {
		return nil, fmt.Errorf("to, subject, and body required")
	}

	raw := fmt.Sprintf("To: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		to, subject, body)
	encoded := base64.URLEncoding.EncodeToString([]byte(raw))
	payload, _ := json.Marshal(map[string]string{"raw": encoded})

	data, err := g.apiPost("https://www.googleapis.com/gmail/v1/users/me/messages/send", payload)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	var resp map[string]any
	json.Unmarshal(data, &resp)
	return map[string]any{"status": "sent", "id": resp["id"], "to": to, "subject": subject}, nil
}

func (g *GoogleIntegration) gmailLabels(params map[string]any) (any, error) {
	data, err := g.apiGet("https://www.googleapis.com/gmail/v1/users/me/labels")
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
			"id": label["id"], "name": label["name"], "type": label["type"],
		})
	}
	return map[string]any{"labels": result}, nil
}

func (g *GoogleIntegration) gmailMarkRead(params map[string]any) (any, error) {
	msgID, _ := params["message_id"].(string)
	if msgID == "" {
		return nil, fmt.Errorf("message_id required")
	}
	payload, _ := json.Marshal(map[string]any{"removeLabelIds": []string{"UNREAD"}})
	_, err := g.apiPost(fmt.Sprintf("https://www.googleapis.com/gmail/v1/users/me/messages/%s/modify", msgID), payload)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return map[string]any{"status": "marked_read", "message_id": msgID}, nil
}

// ══════════════════════════════════════════════════════════════
// ── GOOGLE CALENDAR TOOLS ────────────────────────────────────
// ══════════════════════════════════════════════════════════════

func (g *GoogleIntegration) calendarTools() []tools.ToolSpec {
	return []tools.ToolSpec{
		{
			Name:        "gcal_today",
			Description: "Get today's calendar events",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"calendar_id": map[string]any{"type": "string", "description": "Calendar ID (default: primary)"},
				},
			},
			Handler: g.calToday,
		},
		{
			Name:        "gcal_upcoming",
			Description: "Get upcoming calendar events for the next N days",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"days":        map[string]any{"type": "integer", "description": "Number of days to look ahead (default: 7)"},
					"calendar_id": map[string]any{"type": "string", "description": "Calendar ID (default: primary)"},
				},
			},
			Handler: g.calUpcoming,
		},
		{
			Name:        "gcal_create",
			Description: "Create a new calendar event",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"summary":     map[string]any{"type": "string", "description": "Event title"},
					"start":       map[string]any{"type": "string", "description": "Start time in ISO 8601 format (e.g. 2026-03-20T10:00:00+03:00)"},
					"end":         map[string]any{"type": "string", "description": "End time in ISO 8601 format"},
					"description": map[string]any{"type": "string", "description": "Event description (optional)"},
					"location":    map[string]any{"type": "string", "description": "Event location (optional)"},
					"calendar_id": map[string]any{"type": "string", "description": "Calendar ID (default: primary)"},
				},
				"required": []string{"summary", "start", "end"},
			},
			Handler: g.calCreate,
		},
		{
			Name:        "gcal_delete",
			Description: "Delete/cancel a calendar event",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"event_id":    map[string]any{"type": "string", "description": "Event ID to delete"},
					"calendar_id": map[string]any{"type": "string", "description": "Calendar ID (default: primary)"},
				},
				"required": []string{"event_id"},
			},
			Handler: g.calDelete,
		},
		{
			Name:        "gcal_calendars",
			Description: "List all available Google calendars",
			Parameters: map[string]any{"type": "object", "properties": map[string]any{}},
			Handler:     g.calList,
		},
	}
}

func (g *GoogleIntegration) calToday(params map[string]any) (any, error) {
	calID := getCalendarID(params)
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	end := start.Add(24 * time.Hour)
	return g.calEvents(calID, start, end)
}

func (g *GoogleIntegration) calUpcoming(params map[string]any) (any, error) {
	calID := getCalendarID(params)
	days := 7
	if d, ok := params["days"].(float64); ok && d > 0 {
		days = int(d)
		if days > 30 {
			days = 30
		}
	}
	now := time.Now()
	end := now.Add(time.Duration(days) * 24 * time.Hour)
	return g.calEvents(calID, now, end)
}

func (g *GoogleIntegration) calEvents(calID string, start, end time.Time) (any, error) {
	path := fmt.Sprintf("https://www.googleapis.com/calendar/v3/calendars/%s/events?timeMin=%s&timeMax=%s&singleEvents=true&orderBy=startTime&maxResults=20",
		url.PathEscape(calID),
		url.QueryEscape(start.Format(time.RFC3339)),
		url.QueryEscape(end.Format(time.RFC3339)))

	data, err := g.apiGet(path)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	var resp map[string]any
	json.Unmarshal(data, &resp)

	rawItems, _ := resp["items"].([]any)
	var events []map[string]any
	for _, raw := range rawItems {
		item, _ := raw.(map[string]any)
		event := map[string]any{
			"id":      item["id"],
			"summary": item["summary"],
			"status":  item["status"],
		}
		if loc, ok := item["location"].(string); ok {
			event["location"] = loc
		}
		if s, ok := item["start"].(map[string]any); ok {
			if dt, ok := s["dateTime"].(string); ok {
				event["start"] = dt
			} else if d, ok := s["date"].(string); ok {
				event["start"] = d
				event["all_day"] = true
			}
		}
		if e, ok := item["end"].(map[string]any); ok {
			if dt, ok := e["dateTime"].(string); ok {
				event["end"] = dt
			} else if d, ok := e["date"].(string); ok {
				event["end"] = d
			}
		}
		events = append(events, event)
	}
	return map[string]any{"events": events, "count": len(events)}, nil
}

func (g *GoogleIntegration) calCreate(params map[string]any) (any, error) {
	summary, _ := params["summary"].(string)
	startStr, _ := params["start"].(string)
	endStr, _ := params["end"].(string)
	if summary == "" || startStr == "" || endStr == "" {
		return nil, fmt.Errorf("summary, start, and end required")
	}
	calID := getCalendarID(params)

	event := map[string]any{
		"summary": summary,
		"start":   map[string]string{"dateTime": startStr},
		"end":     map[string]string{"dateTime": endStr},
	}
	if desc, ok := params["description"].(string); ok && desc != "" {
		event["description"] = desc
	}
	if loc, ok := params["location"].(string); ok && loc != "" {
		event["location"] = loc
	}

	payload, _ := json.Marshal(event)
	path := fmt.Sprintf("https://www.googleapis.com/calendar/v3/calendars/%s/events", url.PathEscape(calID))
	data, err := g.apiPost(path, payload)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	var resp map[string]any
	json.Unmarshal(data, &resp)
	return map[string]any{"status": "created", "id": resp["id"], "summary": summary, "link": resp["htmlLink"]}, nil
}

func (g *GoogleIntegration) calDelete(params map[string]any) (any, error) {
	eventID, _ := params["event_id"].(string)
	if eventID == "" {
		return nil, fmt.Errorf("event_id required")
	}
	calID := getCalendarID(params)
	path := fmt.Sprintf("https://www.googleapis.com/calendar/v3/calendars/%s/events/%s", url.PathEscape(calID), url.PathEscape(eventID))
	err := g.apiDelete(path)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return map[string]any{"status": "deleted", "event_id": eventID}, nil
}

func (g *GoogleIntegration) calList(params map[string]any) (any, error) {
	data, err := g.apiGet("https://www.googleapis.com/calendar/v3/users/me/calendarList")
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	var resp map[string]any
	json.Unmarshal(data, &resp)
	rawItems, _ := resp["items"].([]any)
	var calendars []map[string]any
	for _, raw := range rawItems {
		item, _ := raw.(map[string]any)
		calendars = append(calendars, map[string]any{
			"id":      item["id"],
			"summary": item["summary"],
			"primary": item["primary"],
		})
	}
	return map[string]any{"calendars": calendars}, nil
}

// ══════════════════════════════════════════════════════════════
// ── GOOGLE DRIVE TOOLS ───────────────────────────────────────
// ══════════════════════════════════════════════════════════════

func (g *GoogleIntegration) driveTools() []tools.ToolSpec {
	return []tools.ToolSpec{
		{
			Name:        "gdrive_search",
			Description: "Search for files and folders in Google Drive",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "Search query (file name or content)"},
					"limit": map[string]any{"type": "integer", "description": "Number of results (default: 10, max: 20)"},
				},
				"required": []string{"query"},
			},
			Handler: g.driveSearch,
		},
		{
			Name:        "gdrive_list",
			Description: "List files in a Google Drive folder",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"folder_id": map[string]any{"type": "string", "description": "Folder ID (default: root)"},
					"limit":     map[string]any{"type": "integer", "description": "Number of results (default: 20)"},
				},
			},
			Handler: g.driveList,
		},
		{
			Name:        "gdrive_read",
			Description: "Read the text content of a Google Doc, Sheet, or text file from Drive",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_id": map[string]any{"type": "string", "description": "File ID to read"},
				},
				"required": []string{"file_id"},
			},
			Handler: g.driveRead,
		},
		{
			Name:        "gdrive_info",
			Description: "Get detailed information about a file in Google Drive",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_id": map[string]any{"type": "string", "description": "File ID"},
				},
				"required": []string{"file_id"},
			},
			Handler: g.driveInfo,
		},
		{
			Name:        "gdrive_share",
			Description: "Generate a shareable link for a file in Google Drive",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file_id": map[string]any{"type": "string", "description": "File ID to share"},
					"role":    map[string]any{"type": "string", "description": "Permission role: reader, writer, commenter (default: reader)"},
				},
				"required": []string{"file_id"},
			},
			Handler: g.driveShare,
		},
	}
}

func (g *GoogleIntegration) driveSearch(params map[string]any) (any, error) {
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

	q := fmt.Sprintf("name contains '%s' and trashed = false", strings.ReplaceAll(query, "'", "\\'"))
	path := fmt.Sprintf("https://www.googleapis.com/drive/v3/files?q=%s&pageSize=%d&fields=files(id,name,mimeType,size,modifiedTime,owners)",
		url.QueryEscape(q), limit)

	data, err := g.apiGet(path)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	var resp map[string]any
	json.Unmarshal(data, &resp)
	rawFiles, _ := resp["files"].([]any)
	return map[string]any{"files": parseDriveFiles(rawFiles), "count": len(rawFiles)}, nil
}

func (g *GoogleIntegration) driveList(params map[string]any) (any, error) {
	folderID := "root"
	if fid, ok := params["folder_id"].(string); ok && fid != "" {
		folderID = fid
	}
	limit := 20
	if l, ok := params["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}

	q := fmt.Sprintf("'%s' in parents and trashed = false", folderID)
	path := fmt.Sprintf("https://www.googleapis.com/drive/v3/files?q=%s&pageSize=%d&fields=files(id,name,mimeType,size,modifiedTime)&orderBy=folder,name",
		url.QueryEscape(q), limit)

	data, err := g.apiGet(path)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	var resp map[string]any
	json.Unmarshal(data, &resp)
	rawFiles, _ := resp["files"].([]any)
	return map[string]any{"files": parseDriveFiles(rawFiles), "folder_id": folderID}, nil
}

func (g *GoogleIntegration) driveRead(params map[string]any) (any, error) {
	fileID, _ := params["file_id"].(string)
	if fileID == "" {
		return nil, fmt.Errorf("file_id required")
	}

	// First get file info to determine type
	info, err := g.apiGet(fmt.Sprintf("https://www.googleapis.com/drive/v3/files/%s?fields=id,name,mimeType", fileID))
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	var fileMeta map[string]any
	json.Unmarshal(info, &fileMeta)
	mimeType, _ := fileMeta["mimeType"].(string)

	var content []byte
	switch {
	case mimeType == "application/vnd.google-apps.document":
		content, err = g.apiGet(fmt.Sprintf("https://www.googleapis.com/drive/v3/files/%s/export?mimeType=text/plain", fileID))
	case mimeType == "application/vnd.google-apps.spreadsheet":
		content, err = g.apiGet(fmt.Sprintf("https://www.googleapis.com/drive/v3/files/%s/export?mimeType=text/csv", fileID))
	case mimeType == "application/vnd.google-apps.presentation":
		content, err = g.apiGet(fmt.Sprintf("https://www.googleapis.com/drive/v3/files/%s/export?mimeType=text/plain", fileID))
	case strings.HasPrefix(mimeType, "text/"):
		content, err = g.apiGet(fmt.Sprintf("https://www.googleapis.com/drive/v3/files/%s?alt=media", fileID))
	default:
		return map[string]any{"error": fmt.Sprintf("Cannot read binary file type: %s", mimeType)}, nil
	}

	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	text := string(content)
	if len(text) > 3000 {
		text = text[:3000] + "...(truncated)"
	}

	return map[string]any{
		"name":      fileMeta["name"],
		"mime_type": mimeType,
		"content":   text,
	}, nil
}

func (g *GoogleIntegration) driveInfo(params map[string]any) (any, error) {
	fileID, _ := params["file_id"].(string)
	if fileID == "" {
		return nil, fmt.Errorf("file_id required")
	}

	data, err := g.apiGet(fmt.Sprintf("https://www.googleapis.com/drive/v3/files/%s?fields=id,name,mimeType,size,modifiedTime,createdTime,owners,shared,webViewLink", fileID))
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	var file map[string]any
	json.Unmarshal(data, &file)

	result := map[string]any{
		"id":        file["id"],
		"name":      file["name"],
		"mime_type": file["mimeType"],
		"size":      file["size"],
		"modified":  file["modifiedTime"],
		"created":   file["createdTime"],
		"shared":    file["shared"],
		"link":      file["webViewLink"],
	}

	if owners, ok := file["owners"].([]any); ok && len(owners) > 0 {
		owner, _ := owners[0].(map[string]any)
		result["owner"] = owner["displayName"]
	}
	return result, nil
}

func (g *GoogleIntegration) driveShare(params map[string]any) (any, error) {
	fileID, _ := params["file_id"].(string)
	if fileID == "" {
		return nil, fmt.Errorf("file_id required")
	}
	role := "reader"
	if r, ok := params["role"].(string); ok && r != "" {
		role = r
	}

	payload, _ := json.Marshal(map[string]any{
		"role": role,
		"type": "anyone",
	})

	path := fmt.Sprintf("https://www.googleapis.com/drive/v3/files/%s/permissions", fileID)
	_, err := g.apiPost(path, payload)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	return map[string]any{
		"status":  "shared",
		"file_id": fileID,
		"role":    role,
		"link":    fmt.Sprintf("https://drive.google.com/file/d/%s/view", fileID),
	}, nil
}

// ══════════════════════════════════════════════════════════════
// ── OAUTH2 TOKEN MANAGEMENT ─────────────────────────────────
// ══════════════════════════════════════════════════════════════

func (g *GoogleIntegration) refreshAccessToken() error {
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

func (g *GoogleIntegration) getToken() (string, error) {
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

// ══════════════════════════════════════════════════════════════
// ── HTTP HELPERS ─────────────────────────────────────────────
// ══════════════════════════════════════════════════════════════

func (g *GoogleIntegration) apiGet(url string) ([]byte, error) {
	token, err := g.getToken()
	if err != nil {
		return nil, err
	}
	req, _ := http.NewRequest("GET", url, nil)
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

func (g *GoogleIntegration) apiPost(url string, data []byte) ([]byte, error) {
	token, err := g.getToken()
	if err != nil {
		return nil, err
	}
	var bodyReader io.Reader
	if data != nil {
		bodyReader = bytes.NewReader(data)
	}
	req, _ := http.NewRequest("POST", url, bodyReader)
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

func (g *GoogleIntegration) apiDelete(url string) error {
	token, err := g.getToken()
	if err != nil {
		return err
	}
	req, _ := http.NewRequest("DELETE", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// ══════════════════════════════════════════════════════════════
// ── HELPERS ──────────────────────────────────────────────────
// ══════════════════════════════════════════════════════════════

func extractBody(payload map[string]any) string {
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
	if parts, ok := payload["parts"].([]any); ok {
		for _, part := range parts {
			p, _ := part.(map[string]any)
			mimeType, _ := p["mimeType"].(string)
			if mimeType == "text/plain" {
				return extractBody(p)
			}
		}
		for _, part := range parts {
			p, _ := part.(map[string]any)
			mimeType, _ := p["mimeType"].(string)
			if strings.HasPrefix(mimeType, "text/") {
				return extractBody(p)
			}
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

func getCalendarID(params map[string]any) string {
	if id, ok := params["calendar_id"].(string); ok && id != "" {
		return id
	}
	return "primary"
}

func parseDriveFiles(rawFiles []any) []map[string]any {
	var files []map[string]any
	for _, raw := range rawFiles {
		f, _ := raw.(map[string]any)
		entry := map[string]any{
			"id":        f["id"],
			"name":      f["name"],
			"mime_type": f["mimeType"],
			"modified":  f["modifiedTime"],
		}
		if size, ok := f["size"].(string); ok {
			entry["size"] = size
		}
		// Indicate if it's a folder
		if f["mimeType"] == "application/vnd.google-apps.folder" {
			entry["type"] = "folder"
		} else {
			entry["type"] = "file"
		}
		files = append(files, entry)
	}
	return files
}

func decodeHeader(s string) string {
	dec := new(mime.WordDecoder)
	result, err := dec.DecodeHeader(s)
	if err != nil {
		return s
	}
	return result
}
