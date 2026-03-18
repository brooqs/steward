// Steward Satellite Client
//
// This is the lightweight client that runs on user machines to connect
// to the central Steward server. It provides:
//   - Text-based chat interaction
//   - Push-to-talk voice input (Enter to record, Enter to send)
//   - Wake word detection ("Hey Steward" always-listening mode)
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
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"sync"
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
	wakeWord := flag.String("wake-word", "steward", "wake word for always-listening mode")
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
		wakeWord:  strings.ToLower(*wakeWord),
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
	wakeWord  string
	conn      *websocket.Conn
	writeMu   sync.Mutex // protects concurrent writes to websocket
	listening bool       // wake word mode active
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
		fmt.Println("─────────────────────────────────────────")
		fmt.Println("Commands:")
		fmt.Println("  /voice    — Push-to-talk (press Enter to record, Enter to send)")
		fmt.Println("  /listen   — Wake word mode (say 'Hey Steward' to activate)")
		fmt.Println("  /stop     — Stop wake word listening")
		fmt.Println("  /sysinfo  — Send system info")
		fmt.Println("  quit      — Exit")
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

		// Commands
		switch text {
		case "/voice":
			c.pushToTalk(ctx)
			fmt.Print("> ")
			continue
		case "/listen":
			c.startWakeWordMode(ctx)
			fmt.Print("> ")
			continue
		case "/stop":
			c.listening = false
			fmt.Println("🔇 Wake word mode stopped")
			fmt.Print("> ")
			continue
		case "/sysinfo":
			c.sendSystemInfo()
			fmt.Print("> ")
			continue
		}

		// Send text message
		c.sendText(text)
	}
}

func (c *SatelliteClient) sendText(text string) {
	msg := Message{
		Type:      "text",
		ID:        fmt.Sprintf("msg_%d", time.Now().UnixNano()),
		Timestamp: time.Now(),
		Payload:   map[string]any{"text": text},
	}
	data, _ := json.Marshal(msg)
	c.writeMu.Lock()
	c.conn.WriteMessage(websocket.TextMessage, data)
	c.writeMu.Unlock()
}

func (c *SatelliteClient) sendAudio(audioData []byte, format string) {
	msg := Message{
		Type:        "audio",
		ID:          fmt.Sprintf("audio_%d", time.Now().UnixNano()),
		Timestamp:   time.Now(),
		AudioFormat: format,
		Payload: map[string]any{
			"audio_base64": base64.StdEncoding.EncodeToString(audioData),
		},
	}
	data, _ := json.Marshal(msg)
	c.writeMu.Lock()
	c.conn.WriteMessage(websocket.TextMessage, data)
	c.writeMu.Unlock()
}

// ── Push-to-Talk ──────────────────────────────────────────────

func (c *SatelliteClient) pushToTalk(ctx context.Context) {
	fmt.Println("🎙️  Recording... Press Enter to stop and send.")

	audioData, err := c.recordUntilEnter(ctx)
	if err != nil {
		fmt.Printf("❌ Recording failed: %s\n", err)
		return
	}

	if len(audioData) < 1000 {
		fmt.Println("⚠️  Recording too short, discarded")
		return
	}

	fmt.Println("📤 Sending audio...")
	c.sendAudio(audioData, "wav")
}

func (c *SatelliteClient) recordUntilEnter(ctx context.Context) ([]byte, error) {
	tmpFile := fmt.Sprintf("/tmp/steward_rec_%d.wav", time.Now().UnixNano())

	// Detect recording tool
	recorder, args := c.getRecorder(tmpFile)
	if recorder == "" {
		return nil, fmt.Errorf("no recording tool found (install arecord, sox, or ffmpeg)")
	}

	cmd := exec.CommandContext(ctx, recorder, args...)
	cmd.Stderr = nil // suppress recorder output

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting recorder: %w", err)
	}

	// Wait for Enter key in a goroutine
	done := make(chan struct{})
	go func() {
		bufio.NewReader(os.Stdin).ReadBytes('\n')
		close(done)
	}()

	select {
	case <-done:
		// User pressed Enter — stop recording
		cmd.Process.Signal(syscall.SIGINT)
		time.Sleep(200 * time.Millisecond)
		cmd.Process.Kill()
	case <-ctx.Done():
		cmd.Process.Kill()
		return nil, ctx.Err()
	}

	cmd.Wait()

	// Read recorded file
	data, err := os.ReadFile(tmpFile)
	os.Remove(tmpFile)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (c *SatelliteClient) getRecorder(outputFile string) (string, []string) {
	// arecord (ALSA)
	if _, err := exec.LookPath("arecord"); err == nil {
		return "arecord", []string{"-f", "S16_LE", "-r", "16000", "-c", "1", "-t", "wav", outputFile}
	}
	// sox (rec)
	if _, err := exec.LookPath("rec"); err == nil {
		return "rec", []string{"-r", "16000", "-c", "1", "-b", "16", outputFile}
	}
	// ffmpeg
	if _, err := exec.LookPath("ffmpeg"); err == nil {
		if runtime.GOOS == "linux" {
			return "ffmpeg", []string{"-y", "-f", "pulse", "-i", "default", "-ar", "16000", "-ac", "1", "-t", "30", outputFile}
		} else if runtime.GOOS == "darwin" {
			return "ffmpeg", []string{"-y", "-f", "avfoundation", "-i", ":0", "-ar", "16000", "-ac", "1", "-t", "30", outputFile}
		}
	}
	return "", nil
}

// ── Wake Word Mode ────────────────────────────────────────────

func (c *SatelliteClient) startWakeWordMode(ctx context.Context) {
	if c.listening {
		fmt.Println("🎧 Already listening!")
		return
	}

	ww := c.wakeWord
	fmt.Printf("🎧 Wake word mode ON — say 'Hey %s' or '%s' to activate\n", ww, ww)
	fmt.Println("   Type /stop to exit wake word mode")
	c.listening = true

	go c.wakeWordLoop(ctx)
}

func (c *SatelliteClient) wakeWordLoop(ctx context.Context) {
	for c.listening {
		select {
		case <-ctx.Done():
			c.listening = false
			return
		default:
		}

		// Record a short chunk (3 seconds)
		audioData, err := c.recordChunk(ctx, 3)
		if err != nil {
			slog.Debug("wake word chunk error", "error", err)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		// Check if there's actually speech (energy-based VAD)
		if !hasVoice(audioData) {
			slog.Debug("silence detected, skipping")
			continue
		}

		slog.Debug("voice detected, sending for STT")

		// Send as wake-word check audio
		msg := Message{
			Type:        "wake_check",
			ID:          fmt.Sprintf("wk_%d", time.Now().UnixNano()),
			Timestamp:   time.Now(),
			AudioFormat: "wav",
			Payload: map[string]any{
				"audio_base64": base64.StdEncoding.EncodeToString(audioData),
				"wake_word":    c.wakeWord,
			},
		}
		data, _ := json.Marshal(msg)
		c.writeMu.Lock()
		c.conn.WriteMessage(websocket.TextMessage, data)
		c.writeMu.Unlock()
	}
}

func (c *SatelliteClient) recordChunk(ctx context.Context, seconds int) ([]byte, error) {
	tmpFile := fmt.Sprintf("/tmp/steward_wk_%d.wav", time.Now().UnixNano())
	defer os.Remove(tmpFile)

	recorder, args := c.getRecorderTimed(tmpFile, seconds)
	if recorder == "" {
		return nil, fmt.Errorf("no recorder available")
	}

	cmd := exec.CommandContext(ctx, recorder, args...)
	cmd.Stderr = nil

	if err := cmd.Run(); err != nil {
		return nil, err
	}

	return os.ReadFile(tmpFile)
}

func (c *SatelliteClient) getRecorderTimed(outputFile string, seconds int) (string, []string) {
	dur := fmt.Sprintf("%d", seconds)

	if _, err := exec.LookPath("arecord"); err == nil {
		return "arecord", []string{"-f", "S16_LE", "-r", "16000", "-c", "1", "-t", "wav", "-d", dur, outputFile}
	}
	if _, err := exec.LookPath("rec"); err == nil {
		return "rec", []string{"-r", "16000", "-c", "1", "-b", "16", outputFile, "trim", "0", dur}
	}
	if _, err := exec.LookPath("ffmpeg"); err == nil {
		if runtime.GOOS == "linux" {
			return "ffmpeg", []string{"-y", "-f", "pulse", "-i", "default", "-ar", "16000", "-ac", "1", "-t", dur, outputFile}
		}
	}
	return "", nil
}

// hasVoice performs simple energy-based Voice Activity Detection.
// Returns true if the audio's RMS energy exceeds a threshold.
func hasVoice(wavData []byte) bool {
	// WAV header is 44 bytes, pcm data follows
	if len(wavData) < 100 {
		return false
	}

	pcmData := wavData[44:] // skip WAV header
	if len(pcmData) < 2 {
		return false
	}

	var sumSquares float64
	sampleCount := len(pcmData) / 2

	for i := 0; i < len(pcmData)-1; i += 2 {
		sample := int16(binary.LittleEndian.Uint16(pcmData[i : i+2]))
		sumSquares += float64(sample) * float64(sample)
	}

	rms := math.Sqrt(sumSquares / float64(sampleCount))

	// Threshold: ~300 RMS is typical for speech, ~50 for silence
	threshold := 200.0
	slog.Debug("VAD check", "rms", rms, "threshold", threshold, "has_voice", rms > threshold)
	return rms > threshold
}

// ── Existing Functionality ────────────────────────────────────

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
			c.writeMu.Lock()
			c.conn.WriteMessage(websocket.TextMessage, data)
			c.writeMu.Unlock()
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

	result := Message{
		Type:      "cmd_result",
		Timestamp: time.Now(),
		Payload: map[string]any{
			"command":   command,
			"stdout":    truncStr(stdout.String(), 65536),
			"stderr":    truncStr(stderr.String(), 65536),
			"exit_code": exitCode,
		},
	}
	data, _ := json.Marshal(result)
	c.writeMu.Lock()
	c.conn.WriteMessage(websocket.TextMessage, data)
	c.writeMu.Unlock()
}

func (c *SatelliteClient) sendSystemInfo() {
	hostname, _ := os.Hostname()

	info := Message{
		Type:      "sys_info",
		Timestamp: time.Now(),
		Payload: map[string]any{
			"hostname":   hostname,
			"os":         runtime.GOOS,
			"arch":       runtime.GOARCH,
			"cpus":       runtime.NumCPU(),
			"goroutines": runtime.NumGoroutine(),
		},
	}

	if dfOut, err := exec.Command("df", "-h", "/").Output(); err == nil {
		info.Payload["disk_info"] = string(dfOut)
	}
	if runtime.GOOS == "linux" {
		if memOut, err := exec.Command("free", "-h").Output(); err == nil {
			info.Payload["memory_info"] = string(memOut)
		}
		if uptimeOut, err := exec.Command("uptime").Output(); err == nil {
			info.Payload["uptime"] = strings.TrimSpace(string(uptimeOut))
		}
	}

	data, _ := json.Marshal(info)
	c.writeMu.Lock()
	c.conn.WriteMessage(websocket.TextMessage, data)
	c.writeMu.Unlock()
	slog.Info("system info sent")
}

func (c *SatelliteClient) playAudio(audioB64, format string) {
	audioData, err := base64.StdEncoding.DecodeString(audioB64)
	if err != nil {
		slog.Error("failed to decode audio", "error", err)
		return
	}

	if format == "" {
		format = "mp3"
	}

	tmpFile := fmt.Sprintf("/tmp/steward_play_%d.%s", time.Now().UnixNano(), format)
	if err := os.WriteFile(tmpFile, audioData, 0o600); err != nil {
		slog.Error("failed to write audio file", "error", err)
		return
	}
	defer os.Remove(tmpFile)

	players := []struct {
		cmd  string
		args []string
	}{
		{"mpv", []string{"--no-video", "--really-quiet", tmpFile}},
		{"ffplay", []string{"-nodisp", "-autoexit", "-loglevel", "quiet", tmpFile}},
		{"aplay", []string{tmpFile}},
		{"paplay", []string{tmpFile}},
		{"afplay", []string{tmpFile}}, // macOS
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

func truncStr(s string, maxLen int) string {
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
