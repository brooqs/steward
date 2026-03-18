// Package satellite also provides tools that allow the LLM to interact
// with connected satellites (list, run commands, request system info).
package satellite

import (
	"encoding/json"
	"fmt"

	"github.com/brooqs/steward/internal/tools"
)

// GetTools returns the satellite management tools for the LLM.
func (s *Server) GetTools() []tools.ToolSpec {
	return []tools.ToolSpec{
		{
			Name:        "satellite_list",
			Description: "List all connected satellite clients with their hostname, OS, architecture and connection time.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
				"required":   []string{},
			},
			Handler: s.toolListSatellites,
		},
		{
			Name:        "satellite_exec",
			Description: "Execute a shell command on a remote satellite machine. Use satellite_list first to get satellite IDs.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"satellite_id": map[string]any{"type": "string", "description": "Satellite ID from satellite_list"},
					"command":      map[string]any{"type": "string", "description": "Shell command to execute"},
					"working_dir":  map[string]any{"type": "string", "description": "Optional working directory"},
				},
				"required": []string{"satellite_id", "command"},
			},
			Handler: s.toolSatelliteExec,
		},
		{
			Name:        "satellite_sysinfo",
			Description: "Request system information (CPU, memory, disk usage) from a satellite.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"satellite_id": map[string]any{"type": "string", "description": "Satellite ID from satellite_list"},
				},
				"required": []string{"satellite_id"},
			},
			Handler: s.toolSatelliteSysInfo,
		},
	}
}

func (s *Server) toolListSatellites(params map[string]any) (any, error) {
	infos := s.ListSatellites()
	if len(infos) == 0 {
		return map[string]any{"satellites": []any{}, "message": "No satellites connected"}, nil
	}

	var result []map[string]any
	for _, info := range infos {
		result = append(result, map[string]any{
			"id":           info.ID,
			"hostname":     info.Hostname,
			"os":           info.OS,
			"arch":         info.Arch,
			"connected_at": info.ConnectedAt.Format("2006-01-02 15:04:05"),
			"last_seen":    info.LastSeen.Format("2006-01-02 15:04:05"),
		})
	}
	return map[string]any{"satellites": result, "count": len(result)}, nil
}

func (s *Server) toolSatelliteExec(params map[string]any) (any, error) {
	satID, _ := params["satellite_id"].(string)
	command, _ := params["command"].(string)
	workDir, _ := params["working_dir"].(string)

	if satID == "" || command == "" {
		return nil, fmt.Errorf("satellite_id and command required")
	}

	if err := s.SendCommand(satID, command, workDir); err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	return map[string]any{
		"status":  "command_sent",
		"message": fmt.Sprintf("Command sent to %s. Result will arrive asynchronously.", satID),
	}, nil
}

func (s *Server) toolSatelliteSysInfo(params map[string]any) (any, error) {
	satID, _ := params["satellite_id"].(string)
	if satID == "" {
		return nil, fmt.Errorf("satellite_id required")
	}

	if err := s.RequestSysInfo(satID); err != nil {
		return map[string]any{"error": err.Error()}, nil
	}

	// Return current cached info
	s.mu.RLock()
	cc, ok := s.satellites[satID]
	s.mu.RUnlock()
	if !ok {
		return map[string]any{"error": "satellite not found"}, nil
	}

	info, _ := json.Marshal(cc.info)
	var result map[string]any
	json.Unmarshal(info, &result)
	result["message"] = "System info requested. Fresh data will arrive shortly."
	return result, nil
}
