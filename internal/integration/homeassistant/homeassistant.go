package homeassistant

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/brooqs/steward/internal/integration"
	"github.com/brooqs/steward/internal/tools"
)

func init() {
	integration.Register("homeassistant", func() integration.Integration {
		return &HAIntegration{}
	})
}

// HAIntegration integrates with Home Assistant REST API.
type HAIntegration struct {
	url     string
	token   string
	enabled bool
	client  *http.Client
}

func (h *HAIntegration) Name() string       { return "homeassistant" }
func (h *HAIntegration) Enabled() bool      { return h.enabled }
func (h *HAIntegration) ToolPrefix() string  { return "ha_" }

func (h *HAIntegration) LoadConfig(cfg map[string]any) error {
	url, _ := cfg["url"].(string)
	token, _ := cfg["token"].(string)
	if url == "" || token == "" {
		return fmt.Errorf("homeassistant requires 'url' and 'token'")
	}
	h.url = strings.TrimRight(url, "/")
	h.token = token
	h.enabled = true
	h.client = &http.Client{Timeout: 10 * time.Second}
	return nil
}

func (h *HAIntegration) HealthCheck() bool {
	if !h.enabled {
		return false
	}
	_, err := h.apiGet("/api/")
	return err == nil
}

func (h *HAIntegration) GetTools() []tools.ToolSpec {
	if !h.enabled {
		return nil
	}
	return []tools.ToolSpec{
		{
			Name:        "ha_get_entity_state",
			Description: "Get the current state of a Home Assistant entity (light, sensor, switch, etc.)",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"entity_id": map[string]any{"type": "string", "description": "Entity ID, e.g. 'light.living_room'"},
				},
				"required": []string{"entity_id"},
			},
			Handler: h.getEntityState,
		},
		{
			Name:        "ha_call_service",
			Description: "Call a Home Assistant service to control devices — turn on/off lights, change color (RGB/HS), set brightness, adjust temperature, toggle switches, run scripts, control WLED",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"domain":    map[string]any{"type": "string", "description": "Service domain: light, switch, climate, script, media_player"},
					"service":   map[string]any{"type": "string", "description": "Service name: turn_on, turn_off, toggle"},
					"entity_id": map[string]any{"type": "string", "description": "Target entity ID, e.g. light.wled"},
					"extra":     map[string]any{"type": "object", "description": "Additional service data: rgb_color, hs_color, brightness, color_temp, temperature"},
				},
				"required": []string{"domain", "service"},
			},
			Handler: h.callService,
		},
		{
			Name:        "ha_list_entities",
			Description: "List Home Assistant entities, optionally filtered by domain (light, switch, sensor)",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"domain": map[string]any{"type": "string", "description": "Filter by domain (e.g. 'light'). Leave empty for all."},
				},
				"required": []string{},
			},
			Handler: h.listEntities,
		},
		{
			Name:        "ha_sync_entities",
			Description: "Fetch ALL Home Assistant entities with full attributes (color modes, capabilities, friendly names). Use this to learn about available devices and their capabilities. Results are automatically cached for future use.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"domain": map[string]any{"type": "string", "description": "Filter by domain (e.g. 'light'). Leave empty for all."},
				},
				"required": []string{},
			},
			Handler: h.syncEntities,
		},
	}
}

func (h *HAIntegration) getEntityState(params map[string]any) (any, error) {
	entityID, _ := params["entity_id"].(string)
	if entityID == "" {
		return nil, fmt.Errorf("entity_id required")
	}
	data, err := h.apiGet("/api/states/" + entityID)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	var state map[string]any
	json.Unmarshal(data, &state)
	return map[string]any{
		"entity_id":    state["entity_id"],
		"state":        state["state"],
		"attributes":   state["attributes"],
		"last_updated": state["last_updated"],
	}, nil
}

func (h *HAIntegration) callService(params map[string]any) (any, error) {
	domain, _ := params["domain"].(string)
	service, _ := params["service"].(string)
	if domain == "" || service == "" {
		return nil, fmt.Errorf("domain and service required")
	}
	body := map[string]any{}
	if eid, ok := params["entity_id"].(string); ok && eid != "" {
		body["entity_id"] = eid
	}
	if extra, ok := params["extra"].(map[string]any); ok {
		for k, v := range extra {
			body[k] = v
		}
	}
	data, _ := json.Marshal(body)
	_, err := h.apiPost(fmt.Sprintf("/api/services/%s/%s", domain, service), data)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return map[string]any{"success": true}, nil
}

func (h *HAIntegration) listEntities(params map[string]any) (any, error) {
	domain, _ := params["domain"].(string)
	data, err := h.apiGet("/api/states")
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	var states []map[string]any
	json.Unmarshal(data, &states)

	var result []map[string]any
	for _, s := range states {
		eid, _ := s["entity_id"].(string)
		if domain != "" && !strings.HasPrefix(eid, domain+".") {
			continue
		}
		attrs, _ := s["attributes"].(map[string]any)
		fname, _ := attrs["friendly_name"].(string)
		result = append(result, map[string]any{
			"entity_id":     eid,
			"state":         s["state"],
			"friendly_name": fname,
		})
		if len(result) >= 50 {
			break
		}
	}
	return result, nil
}

func (h *HAIntegration) syncEntities(params map[string]any) (any, error) {
	domain, _ := params["domain"].(string)
	data, err := h.apiGet("/api/states")
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	var states []map[string]any
	json.Unmarshal(data, &states)

	var result []map[string]any
	for _, s := range states {
		eid, _ := s["entity_id"].(string)
		if domain != "" && !strings.HasPrefix(eid, domain+".") {
			continue
		}
		attrs, _ := s["attributes"].(map[string]any)
		fname, _ := attrs["friendly_name"].(string)

		entry := map[string]any{
			"entity_id":     eid,
			"state":         s["state"],
			"friendly_name": fname,
		}

		// Include important attributes for device control
		if colorModes, ok := attrs["supported_color_modes"]; ok {
			entry["supported_color_modes"] = colorModes
		}
		if brightness, ok := attrs["brightness"]; ok {
			entry["brightness"] = brightness
		}
		if minTemp, ok := attrs["min_temp"]; ok {
			entry["min_temp"] = minTemp
		}
		if maxTemp, ok := attrs["max_temp"]; ok {
			entry["max_temp"] = maxTemp
		}
		if features, ok := attrs["supported_features"]; ok {
			entry["supported_features"] = features
		}
		if deviceClass, ok := attrs["device_class"]; ok {
			entry["device_class"] = deviceClass
		}
		if unitOfMeasure, ok := attrs["unit_of_measurement"]; ok {
			entry["unit_of_measurement"] = unitOfMeasure
		}

		result = append(result, entry)
	}

	return map[string]any{
		"total_entities": len(result),
		"entities":       result,
		"message":        fmt.Sprintf("Synced %d entities with full attributes. This data is now cached for future use.", len(result)),
	}, nil
}

func (h *HAIntegration) apiGet(path string) ([]byte, error) {
	req, _ := http.NewRequest("GET", h.url+path, nil)
	req.Header.Set("Authorization", "Bearer "+h.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.client.Do(req)
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

func (h *HAIntegration) apiPost(path string, data []byte) ([]byte, error) {
	req, _ := http.NewRequest("POST", h.url+path, strings.NewReader(string(data)))
	req.Header.Set("Authorization", "Bearer "+h.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.client.Do(req)
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
