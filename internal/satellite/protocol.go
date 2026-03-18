// Package satellite defines the protocol and message types for
// Steward satellite communication.
//
// Architecture:
//
//	┌──────────────────┐    WebSocket (TLS)    ┌───────────────────┐
//	│  Steward Server   │◄────────────────────►│  Satellite Client  │
//	│                   │                      │  (User's PC)       │
//	│  • LLM            │  ● text messages     │                    │
//	│  • Memory         │  ● audio streaming   │  • Microphone      │
//	│  • Integrations   │  ● system commands   │  • Speaker         │
//	│  • Voice (STT/TTS)│  ● heartbeat         │  • Shell access    │
//	└──────────────────┘                      └───────────────────┘
package satellite

import "time"

// MessageType defines the types of messages exchanged between server and satellite.
type MessageType string

const (
	// Client → Server
	TypeAuth       MessageType = "auth"        // authentication handshake
	TypeText       MessageType = "text"        // text chat message
	TypeAudio      MessageType = "audio"       // audio data for STT
	TypeHeartbeat  MessageType = "heartbeat"   // keep-alive
	TypeSysInfo    MessageType = "sys_info"    // system info response
	TypeCmdResult  MessageType = "cmd_result"  // shell command result

	// Server → Client
	TypeAuthOK     MessageType = "auth_ok"     // auth succeeded
	TypeAuthFail   MessageType = "auth_fail"   // auth failed
	TypeReply      MessageType = "reply"       // text reply from assistant
	TypeSpeak      MessageType = "speak"       // audio data for playback (TTS)
	TypeSysRequest MessageType = "sys_request" // request system info
	TypeCmdExec    MessageType = "cmd_exec"    // execute shell command
	TypeError      MessageType = "error"       // error message
)

// Message is the envelope for all satellite communication.
type Message struct {
	Type      MessageType    `json:"type"`
	ID        string         `json:"id,omitempty"`        // message correlation ID
	Timestamp time.Time      `json:"timestamp"`
	Payload   map[string]any `json:"payload,omitempty"`

	// For audio data (binary), stored separately
	AudioData   []byte `json:"-"`
	AudioFormat string `json:"audio_format,omitempty"`
}

// Common payload keys
const (
	KeyText       = "text"
	KeyToken      = "token"
	KeySessionID  = "session_id"
	KeyCommand    = "command"
	KeyWorkingDir = "working_dir"
	KeyExitCode   = "exit_code"
	KeyStdout     = "stdout"
	KeyStderr     = "stderr"
	KeyError      = "error"
	KeyHostname   = "hostname"
	KeyOS         = "os"
	KeyArch       = "arch"
	KeyUptime     = "uptime"
	KeyCPUUsage   = "cpu_usage"
	KeyMemTotal   = "mem_total"
	KeyMemUsed    = "mem_used"
	KeyDiskTotal  = "disk_total"
	KeyDiskUsed   = "disk_used"
)

// SatelliteInfo holds information about a connected satellite.
type SatelliteInfo struct {
	ID        string    `json:"id"`
	Hostname  string    `json:"hostname"`
	OS        string    `json:"os"`
	Arch      string    `json:"arch"`
	ConnectedAt time.Time `json:"connected_at"`
	LastSeen  time.Time `json:"last_seen"`
}
