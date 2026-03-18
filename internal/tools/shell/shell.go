// Package shell provides the shell_exec tool that allows the AI assistant
// to execute commands on the host machine.
//
// SECURITY: This tool is disabled by default and must be explicitly enabled
// in the configuration. It includes command blocklisting, timeout enforcement,
// and output size limits.
package shell

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/brooqs/steward/internal/config"
	"github.com/brooqs/steward/internal/tools"
)

// Shell provides shell command execution capabilities.
type Shell struct {
	cfg config.ShellConfig
}

// New creates a new Shell tool.
func New(cfg config.ShellConfig) *Shell {
	return &Shell{cfg: cfg}
}

// GetTools returns the tool specs if shell is enabled.
func (s *Shell) GetTools() []tools.ToolSpec {
	if !s.cfg.Enabled {
		return nil
	}
	return []tools.ToolSpec{
		{
			Name:        "shell_exec",
			Description: "Execute a shell command on the host machine and return the output. Use this for system administration, file management, package management, service control, and other system tasks. Be careful with destructive commands.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{
						"type":        "string",
						"description": "The shell command to execute",
					},
					"working_dir": map[string]any{
						"type":        "string",
						"description": "Optional working directory for the command",
					},
				},
				"required": []string{"command"},
			},
			Handler: s.exec,
		},
	}
}

// exec runs a shell command with security checks.
func (s *Shell) exec(params map[string]any) (any, error) {
	command, ok := params["command"].(string)
	if !ok || command == "" {
		return nil, fmt.Errorf("command parameter is required")
	}

	// Security: check against blocklist
	if blocked, reason := s.isBlocked(command); blocked {
		return map[string]any{
			"error":   "command blocked",
			"reason":  reason,
			"command": command,
		}, nil
	}

	// Security: check allowed directories if configured
	workDir, _ := params["working_dir"].(string)
	if workDir != "" && len(s.cfg.AllowedDirs) > 0 {
		allowed := false
		for _, dir := range s.cfg.AllowedDirs {
			if strings.HasPrefix(workDir, dir) {
				allowed = true
				break
			}
		}
		if !allowed {
			return map[string]any{
				"error":       "directory not allowed",
				"working_dir": workDir,
				"allowed":     s.cfg.AllowedDirs,
			}, nil
		}
	}

	// Execute with timeout
	timeout := time.Duration(s.cfg.Timeout) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	if workDir != "" {
		cmd.Dir = workDir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	// Truncate output if too large
	outStr := stdout.String()
	errStr := stderr.String()
	maxBytes := s.cfg.MaxOutputBytes
	if len(outStr) > maxBytes {
		outStr = outStr[:maxBytes] + "\n... [output truncated]"
	}
	if len(errStr) > maxBytes {
		errStr = errStr[:maxBytes] + "\n... [stderr truncated]"
	}

	result := map[string]any{
		"stdout":    outStr,
		"stderr":    errStr,
		"exit_code": 0,
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			result["error"] = fmt.Sprintf("command timed out after %ds", s.cfg.Timeout)
			result["exit_code"] = -1
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			result["exit_code"] = exitErr.ExitCode()
		} else {
			result["error"] = err.Error()
			result["exit_code"] = -1
		}
	}

	return result, nil
}

// isBlocked checks if a command matches the blocklist.
func (s *Shell) isBlocked(command string) (bool, string) {
	normalized := strings.TrimSpace(strings.ToLower(command))
	for _, blocked := range s.cfg.BlockedCommands {
		blocked = strings.TrimSpace(strings.ToLower(blocked))
		if strings.Contains(normalized, blocked) {
			return true, fmt.Sprintf("command contains blocked pattern: %s", blocked)
		}
	}

	// Additional hardcoded safety checks
	dangerousPatterns := []string{
		":(){ :|:& };:",    // fork bomb
		"> /dev/sd",        // disk overwrite
		"chmod -r 777 /",   // global permission change
		"chown -r",         // recursive ownership change on root
		"mv / ",            // moving root
	}
	for _, pattern := range dangerousPatterns {
		if strings.Contains(normalized, pattern) {
			return true, fmt.Sprintf("command matches dangerous pattern: %s", pattern)
		}
	}

	return false, ""
}
