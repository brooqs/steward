// Steward Satellite Client
//
// This is the lightweight client that runs on user machines to connect
// to the central Steward server. It provides:
//   - Text-based chat interaction
//   - Audio capture (microphone → server for STT)
//   - Audio playback (server TTS → speaker)
//   - Remote shell command execution
//   - System information reporting
//
// Usage:
//
//	steward-satellite --server ws://steward.local:9090/ws --token mytoken
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

// Message mirrors the server's message type for client-side use.
type Message struct {
	Type        string         `json:"type"`
	ID          string         `json:"id,omitempty"`
	Timestamp   time.Time      `json:"timestamp"`
	Payload     map[string]any `json:"payload,omitempty"`
	AudioFormat string         `json:"audio_format,omitempty"`
}

var (
	version = "dev"
	commit  = "none"
)

func main() {
	serverURL := flag.String("server", "ws://localhost:9090/ws", "Steward server WebSocket URL")
	token := flag.String("token", "", "authentication token")
	logLevel := flag.String("log-level", "info", "log level: debug | info | warn | error")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("steward-satellite %s (%s)\n", version, commit)
		os.Exit(0)
	}

	setupLogging(*logLevel)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		slog.Info("shutting down...")
		cancel()
	}()

	client := &SatelliteClient{
		serverURL: *serverURL,
		token:     *token,
	}

	if err := client.Run(ctx); err != nil {
		slog.Error("satellite exited", "error", err)
		os.Exit(1)
	}
}

// SatelliteClient manages the connection to the Steward server.
type SatelliteClient struct {
	serverURL string
	token     string
	conn      *websocket.Conn
}

// Run connects to the server and enters the main loop.
func (c *SatelliteClient) Run(ctx context.Context) error {
	slog.Info("connecting to steward", "url", c.serverURL)

	conn, _, err := websocket.DefaultDialer.Dial(c.serverURL, nil)
	if err != nil {
		return fmt.Errorf("connecting to server: %w", err)
	}
	c.conn = conn
	defer conn.Close()

	// Authenticate
	if err := c.authenticate(); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	// Start heartbeat
	go c.heartbeatLoop(ctx)

	// Start reading server messages
	go c.readLoop(ctx)

	// Interactive text input
	c.interactiveLoop(ctx)

	return nil
}

func (c *SatelliteClient) authenticate() error {
	hostname, _ := os.Hostname()

	authMsg := Message{
		Type:      "auth",
		Timestamp: time.Now(),
		Payload: map[string]any{
			"token":    c.token,
			"hostname": hostname,
			"os":       runtime.GOOS,
			"arch":     runtime.GOARCH,
		},
	}

	data, _ := json.Marshal(authMsg)
	if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return err
	}

	// Wait for auth response
	_, respData, err := c.conn.ReadMessage()
	if err != nil {
		return err
	}

	var resp Message
	if err := json.Unmarshal(respData, &resp); err != nil {
		return err
	}

	switch resp.Type {
	case "auth_ok":
		satID, _ := resp.Payload["satellite_id"].(string)
		fmt.Printf("🟢 Connected to Steward (satellite: %s)\n", satID)
		fmt.Println("Type a message to chat, or 'quit' to exit.")
		fmt.Println("─────────────────────────────────────────")
		return nil
	case "auth_fail":
		errMsg, _ := resp.Payload["error"].(string)
		return fmt.Errorf("auth rejected: %s", errMsg)
	default:
		return fmt.Errorf("unexpected response: %s", resp.Type)
	}
}

func (c *SatelliteClient) readLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, data, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Error("connection lost", "error", err)
			}
			return
		}

		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		c.handleServerMessage(msg)
	}
}

func (c *SatelliteClient) handleServerMessage(msg Message) {
	switch msg.Type {
	case "reply":
		text, _ := msg.Payload["text"].(string)
		fmt.Printf("\n🤖 %s\n\n> ", text)

	case "speak":
		// Play audio if we receive TTS
		audioB64, _ := msg.Payload["audio_base64"].(string)
		if audioB64 != "" {
			go c.playAudio(audioB64, msg.AudioFormat)
		}

	case "cmd_exec":
		// Server wants us to run a command
		command, _ := msg.Payload["command"].(string)
		workDir, _ := msg.Payload["working_dir"].(string)
		go c.executeCommand(command, workDir)

	case "sys_request":
		go c.sendSystemInfo()

	case "error":
		errMsg, _ := msg.Payload["error"].(string)
		fmt.Printf("\n❌ Server error: %s\n> ", errMsg)

	case "heartbeat":
		// silent

	default:
		slog.Debug("unknown message type", "type", msg.Type)
	}
}

func (c *SatelliteClient) interactiveLoop(ctx context.Context) {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("> ")

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			fmt.Print("> ")
			continue
		}

		if text == "quit" || text == "exit" {
			fmt.Println("👋 Goodbye!")
			return
		}

		// Special commands
		if text == "/sysinfo" {
			c.sendSystemInfo()
			fmt.Print("> ")
			continue
		}

		// Send text message
		msg := Message{
			Type:      "text",
			ID:        fmt.Sprintf("msg_%d", time.Now().UnixNano()),
			Timestamp: time.Now(),
			Payload:   map[string]any{"text": text},
		}
		data, _ := json.Marshal(msg)
		if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
			fmt.Printf("❌ Send failed: %s\n", err)
		}
	}
}

func (c *SatelliteClient) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			msg := Message{Type: "heartbeat", Timestamp: time.Now()}
			data, _ := json.Marshal(msg)
			c.conn.WriteMessage(websocket.TextMessage, data)
		}
	}
}

func (c *SatelliteClient) executeCommand(command, workDir string) {
	slog.Info("executing remote command", "command", command)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	if workDir != "" {
		cmd.Dir = workDir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	exitCode := 0
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	// Send result back
	result := Message{
		Type:      "cmd_result",
		Timestamp: time.Now(),
		Payload: map[string]any{
			"command":   command,
			"stdout":    truncate(stdout.String(), 65536),
			"stderr":    truncate(stderr.String(), 65536),
			"exit_code": exitCode,
		},
	}
	data, _ := json.Marshal(result)
	c.conn.WriteMessage(websocket.TextMessage, data)
}

func (c *SatelliteClient) sendSystemInfo() {
	hostname, _ := os.Hostname()

	info := Message{
		Type:      "sys_info",
		Timestamp: time.Now(),
		Payload: map[string]any{
			"hostname": hostname,
			"os":       runtime.GOOS,
			"arch":     runtime.GOARCH,
			"cpus":     runtime.NumCPU(),
			"goroutines": runtime.NumGoroutine(),
		},
	}

	// Try to get disk usage
	if dfOut, err := exec.Command("df", "-h", "/").Output(); err == nil {
		info.Payload["disk_info"] = string(dfOut)
	}

	// Try to get memory info
	if runtime.GOOS == "linux" {
		if memOut, err := exec.Command("free", "-h").Output(); err == nil {
			info.Payload["memory_info"] = string(memOut)
		}
		if uptimeOut, err := exec.Command("uptime").Output(); err == nil {
			info.Payload["uptime"] = strings.TrimSpace(string(uptimeOut))
		}
	}

	data, _ := json.Marshal(info)
	c.conn.WriteMessage(websocket.TextMessage, data)
	slog.Info("system info sent")
}

func (c *SatelliteClient) playAudio(audioB64, format string) {
	audioData, err := base64.StdEncoding.DecodeString(audioB64)
	if err != nil {
		slog.Error("failed to decode audio", "error", err)
		return
	}

	// Write to temp file
	tmpFile := fmt.Sprintf("/tmp/steward_play_%d.%s", time.Now().UnixNano(), format)
	if err := os.WriteFile(tmpFile, audioData, 0o600); err != nil {
		slog.Error("failed to write audio file", "error", err)
		return
	}
	defer os.Remove(tmpFile)

	// Try various audio players
	players := []struct {
		cmd  string
		args []string
	}{
		{"mpv", []string{"--no-video", "--really-quiet", tmpFile}},
		{"ffplay", []string{"-nodisp", "-autoexit", "-loglevel", "quiet", tmpFile}},
		{"aplay", []string{tmpFile}},     // Linux ALSA
		{"paplay", []string{tmpFile}},    // PulseAudio
		{"afplay", []string{tmpFile}},    // macOS
	}

	for _, p := range players {
		if _, err := exec.LookPath(p.cmd); err == nil {
			cmd := exec.Command(p.cmd, p.args...)
			if err := cmd.Run(); err != nil {
				slog.Debug("player failed", "player", p.cmd, "error", err)
				continue
			}
			return
		}
	}

	slog.Warn("no audio player found, install mpv or ffplay")
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "\n... [truncated]"
}

func setupLogging(level string) {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})
	slog.SetDefault(slog.New(handler))
}
