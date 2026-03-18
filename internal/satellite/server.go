package satellite

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/brooqs/steward/internal/core"
	"github.com/brooqs/steward/internal/voice"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024 * 64,
	WriteBufferSize: 1024 * 64,
	CheckOrigin:     func(r *http.Request) bool { return true }, // TODO: restrict in production
}

// ServerConfig holds satellite server configuration.
type ServerConfig struct {
	ListenAddr string   `yaml:"listen_addr"` // e.g., "0.0.0.0:9090"
	AuthTokens []string `yaml:"auth_tokens"` // allowed satellite tokens
	TLSCert    string   `yaml:"tls_cert"`    // TLS certificate path
	TLSKey     string   `yaml:"tls_key"`     // TLS private key path
}

// Server handles incoming satellite connections via WebSocket.
type Server struct {
	cfg         ServerConfig
	steward     *core.Steward
	voiceEngine *voice.Engine
	tokens      map[string]bool

	mu          sync.RWMutex
	satellites  map[string]*clientConn // id → connection
}

// clientConn represents an active satellite connection.
type clientConn struct {
	info    SatelliteInfo
	conn    *websocket.Conn
	mu      sync.Mutex // protects conn writes
	cancel  context.CancelFunc
}

// NewServer creates a new satellite server.
func NewServer(cfg ServerConfig, steward *core.Steward, voiceEngine *voice.Engine) *Server {
	tokens := make(map[string]bool, len(cfg.AuthTokens))
	for _, t := range cfg.AuthTokens {
		tokens[t] = true
	}
	return &Server{
		cfg:         cfg,
		steward:     steward,
		voiceEngine: voiceEngine,
		tokens:      tokens,
		satellites:  make(map[string]*clientConn),
	}
}

// Run starts the satellite WebSocket server.
func (s *Server) Run(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWebSocket)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("/satellites", s.handleListSatellites)

	srv := &http.Server{
		Addr:    s.cfg.ListenAddr,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	slog.Info("satellite server listening", "addr", s.cfg.ListenAddr)

	if s.cfg.TLSCert != "" && s.cfg.TLSKey != "" {
		return srv.ListenAndServeTLS(s.cfg.TLSCert, s.cfg.TLSKey)
	}
	return srv.ListenAndServe()
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "error", err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	cc := &clientConn{conn: conn, cancel: cancel}

	defer func() {
		cancel()
		conn.Close()
		// Remove from active satellites
		s.mu.Lock()
		for id, c := range s.satellites {
			if c == cc {
				delete(s.satellites, id)
				slog.Info("satellite disconnected", "id", id)
			}
		}
		s.mu.Unlock()
	}()

	// First message must be auth
	if !s.authenticateClient(cc) {
		return
	}

	// Message loop
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Error("websocket read error", "error", err)
			}
			return
		}

		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			s.sendError(cc, "invalid message format")
			continue
		}

		s.handleMessage(ctx, cc, msg, data)
	}
}

func (s *Server) authenticateClient(cc *clientConn) bool {
	cc.conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, data, err := cc.conn.ReadMessage()
	cc.conn.SetReadDeadline(time.Time{}) // reset

	if err != nil {
		return false
	}

	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil || msg.Type != TypeAuth {
		s.sendMsg(cc, Message{Type: TypeAuthFail, Payload: map[string]any{KeyError: "expected auth message"}})
		return false
	}

	token, _ := msg.Payload[KeyToken].(string)
	if len(s.tokens) > 0 && !s.tokens[token] {
		slog.Warn("satellite auth failed", "remote", cc.conn.RemoteAddr())
		s.sendMsg(cc, Message{Type: TypeAuthFail, Payload: map[string]any{KeyError: "invalid token"}})
		return false
	}

	// Extract satellite info
	hostname, _ := msg.Payload[KeyHostname].(string)
	osName, _ := msg.Payload[KeyOS].(string)
	arch, _ := msg.Payload[KeyArch].(string)

	satID := fmt.Sprintf("sat_%s_%d", hostname, time.Now().UnixNano()%10000)
	cc.info = SatelliteInfo{
		ID:          satID,
		Hostname:    hostname,
		OS:          osName,
		Arch:        arch,
		ConnectedAt: time.Now(),
		LastSeen:    time.Now(),
	}

	s.mu.Lock()
	s.satellites[satID] = cc
	s.mu.Unlock()

	slog.Info("satellite authenticated",
		"id", satID,
		"hostname", hostname,
		"os", osName,
		"arch", arch,
	)

	s.sendMsg(cc, Message{
		Type: TypeAuthOK,
		Payload: map[string]any{
			"satellite_id": satID,
			"message":      "Welcome to Steward",
		},
	})
	return true
}

func (s *Server) handleMessage(ctx context.Context, cc *clientConn, msg Message, rawData []byte) {
	cc.info.LastSeen = time.Now()

	switch msg.Type {
	case TypeText:
		go s.handleTextMessage(ctx, cc, msg)
	case TypeAudio:
		go s.handleAudioMessage(ctx, cc, msg, rawData)
	case TypeWakeCheck:
		go s.handleWakeCheck(ctx, cc, msg)
	case TypeHeartbeat:
		s.sendMsg(cc, Message{Type: TypeHeartbeat, Timestamp: time.Now()})
	case TypeSysInfo:
		slog.Info("satellite system info",
			"id", cc.info.ID,
			"cpu", msg.Payload[KeyCPUUsage],
			"mem_used", msg.Payload[KeyMemUsed],
		)
	case TypeCmdResult:
		slog.Info("satellite command result",
			"id", cc.info.ID,
			"exit_code", msg.Payload[KeyExitCode],
		)
	default:
		s.sendError(cc, fmt.Sprintf("unknown message type: %s", msg.Type))
	}
}

func (s *Server) handleTextMessage(ctx context.Context, cc *clientConn, msg Message) {
	text, _ := msg.Payload[KeyText].(string)
	if text == "" {
		return
	}

	sessionID := fmt.Sprintf("satellite:%s", cc.info.ID)
	response, err := s.steward.Chat(ctx, sessionID, text)
	if err != nil {
		slog.Error("steward chat error", "error", err)
		response = "Sorry, something went wrong."
	}

	// Send text reply
	s.sendMsg(cc, Message{
		Type:    TypeReply,
		ID:      msg.ID,
		Payload: map[string]any{KeyText: response},
	})

	// If TTS is available, also send audio
	if s.voiceEngine != nil && s.voiceEngine.HasTTS() {
		audio, err := s.voiceEngine.Speak(ctx, response, nil)
		if err != nil {
			slog.Error("TTS failed", "error", err)
			return
		}
		s.sendMsg(cc, Message{
			Type:        TypeSpeak,
			ID:          msg.ID,
			AudioFormat: "mp3",
			Payload:     map[string]any{"audio_base64": encodeBase64(audio)},
		})
	}
}

func (s *Server) handleAudioMessage(ctx context.Context, cc *clientConn, msg Message, rawData []byte) {
	if s.voiceEngine == nil || !s.voiceEngine.HasSTT() {
		s.sendError(cc, "STT not configured on server")
		return
	}

	// Extract audio data from base64
	audioB64, _ := msg.Payload["audio_base64"].(string)
	if audioB64 == "" {
		s.sendError(cc, "no audio data")
		return
	}
	audioData := decodeBase64(audioB64)

	format := msg.AudioFormat
	if format == "" {
		format = "wav"
	}

	// Transcribe
	text, err := s.voiceEngine.Transcribe(ctx, audioData, format)
	if err != nil {
		slog.Error("STT failed", "error", err)
		s.sendError(cc, "transcription failed")
		return
	}

	slog.Info("satellite audio transcribed",
		"id", cc.info.ID,
		"text", text,
	)

	// Process as text message
	s.handleTextMessage(ctx, cc, Message{
		Type:    TypeText,
		ID:      msg.ID,
		Payload: map[string]any{KeyText: text},
	})
}

func (s *Server) handleWakeCheck(ctx context.Context, cc *clientConn, msg Message) {
	if s.voiceEngine == nil || !s.voiceEngine.HasSTT() {
		return // silently skip if no STT
	}

	audioB64, _ := msg.Payload["audio_base64"].(string)
	if audioB64 == "" {
		return
	}
	audioData := decodeBase64(audioB64)

	format := msg.AudioFormat
	if format == "" {
		format = "wav"
	}

	// Transcribe the short audio chunk
	text, err := s.voiceEngine.Transcribe(ctx, audioData, format)
	if err != nil {
		slog.Debug("wake check STT failed", "error", err)
		return
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return
	}

	wakeWord, _ := msg.Payload["wake_word"].(string)
	if wakeWord == "" {
		wakeWord = "steward"
	}

	lowerText := strings.ToLower(text)
	slog.Debug("wake check transcription", "text", text, "wake_word", wakeWord)

	// Check if wake word is in the transcription
	if !strings.Contains(lowerText, strings.ToLower(wakeWord)) {
		return // no wake word detected
	}

	slog.Info("wake word detected!",
		"satellite", cc.info.ID,
		"text", text,
	)

	// Remove wake word prefix ("hey steward", "steward", etc.) to get the command
	command := lowerText
	for _, prefix := range []string{"hey " + wakeWord, wakeWord} {
		if strings.HasPrefix(command, prefix) {
			command = strings.TrimSpace(text[len(prefix):])
			break
		}
	}

	// If the user only said the wake word with no command, acknowledge
	if strings.TrimSpace(command) == "" || command == lowerText {
		s.sendMsg(cc, Message{
			Type:    TypeReply,
			Payload: map[string]any{KeyText: "Evet, seni dinliyorum?"},
		})
		if s.voiceEngine.HasTTS() {
			if audio, err := s.voiceEngine.Speak(ctx, "Evet, seni dinliyorum?", nil); err == nil {
				s.sendMsg(cc, Message{
					Type:        TypeSpeak,
					AudioFormat: "mp3",
					Payload:     map[string]any{"audio_base64": encodeBase64(audio)},
				})
			}
		}
		return
	}

	// Process the command part
	s.handleTextMessage(ctx, cc, Message{
		Type:    TypeText,
		ID:      msg.ID,
		Payload: map[string]any{KeyText: command},
	})
}

// SendCommand sends a shell command to a satellite for execution.
func (s *Server) SendCommand(satelliteID, command, workingDir string) error {
	s.mu.RLock()
	cc, ok := s.satellites[satelliteID]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("satellite not found: %s", satelliteID)
	}
	return s.sendMsg(cc, Message{
		Type: TypeCmdExec,
		Payload: map[string]any{
			KeyCommand:    command,
			KeyWorkingDir: workingDir,
		},
	})
}

// RequestSysInfo asks a satellite for its current system info.
func (s *Server) RequestSysInfo(satelliteID string) error {
	s.mu.RLock()
	cc, ok := s.satellites[satelliteID]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("satellite not found: %s", satelliteID)
	}
	return s.sendMsg(cc, Message{Type: TypeSysRequest})
}

// ListSatellites returns info about all connected satellites.
func (s *Server) ListSatellites() []SatelliteInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	infos := make([]SatelliteInfo, 0, len(s.satellites))
	for _, cc := range s.satellites {
		infos = append(infos, cc.info)
	}
	return infos
}

func (s *Server) handleListSatellites(w http.ResponseWriter, r *http.Request) {
	infos := s.ListSatellites()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(infos)
}

func (s *Server) sendMsg(cc *clientConn, msg Message) error {
	msg.Timestamp = time.Now()
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	cc.mu.Lock()
	defer cc.mu.Unlock()
	return cc.conn.WriteMessage(websocket.TextMessage, data)
}

func (s *Server) sendError(cc *clientConn, errMsg string) {
	s.sendMsg(cc, Message{
		Type:    TypeError,
		Payload: map[string]any{KeyError: errMsg},
	})
}

// Base64 helpers for audio transport

func encodeBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

func decodeBase64(s string) []byte {
	data, _ := base64.StdEncoding.DecodeString(s)
	return data
}
