package ide

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Config represents IDE integration configuration
type Config struct {
	Enable    bool   `yaml:"enable"`
	Transport string `yaml:"transport"` // websocket or stdio
	DiffTool  string `yaml:"diff_tool"` // auto, vscode, or disabled
	Port      int    `yaml:"port"`      // WebSocket port (default: 8123)
}

// Message represents communication between CLI and IDE extension
type Message struct {
	Type      string                 `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
}

// SelectionMessage represents a text selection in the editor
type SelectionMessage struct {
	Type      string `json:"type"`
	File      string `json:"file"`
	Text      string `json:"text"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Language  string `json:"language,omitempty"`
}

// DiagnosticMessage represents a diagnostic (error/warning) from the IDE
type DiagnosticMessage struct {
	Type     string `json:"type"`
	File     string `json:"file"`
	Message  string `json:"message"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Severity string `json:"severity"` // error, warning, info
}

// FileReferenceMessage represents a file reference with line numbers
type FileReferenceMessage struct {
	Type      string `json:"type"`
	File      string `json:"file"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
	Content   string `json:"content,omitempty"`
}

// IDEContext holds context information from the IDE
type IDEContext struct {
	ActiveFile    string              `json:"active_file,omitempty"`
	Selection     *SelectionMessage   `json:"selection,omitempty"`
	Diagnostics   []DiagnosticMessage `json:"diagnostics,omitempty"`
	OpenFiles     []string            `json:"open_files,omitempty"`
	WorkspaceRoot string              `json:"workspace_root,omitempty"`
}

// DiffResult represents a proposed code change
type DiffResult struct {
	File        string `json:"file"`
	OrigContent string `json:"orig_content"`
	NewContent  string `json:"new_content"`
	Patch       string `json:"patch"`
	Language    string `json:"language,omitempty"`
}

// HandshakeMessage is the magic token for VS Code extension detection
const HandshakeMessage = "###DEVGRU_VSCODE_HANDSHAKE###"

// DiffStartMarker marks the beginning of a diff block
const DiffStartMarker = "<<<DEVGRU_DIFF_START>>>"

// DiffEndMarker marks the end of a diff block
const DiffEndMarker = "<<<DEVGRU_DIFF_END>>>"

// Server handles WebSocket connections from VS Code extension
type Server struct {
	config      Config
	context     *IDEContext
	connections map[*websocket.Conn]bool
	broadcast   chan []byte
	register    chan *websocket.Conn
	unregister  chan *websocket.Conn
	mu          sync.RWMutex
	running     bool
}
