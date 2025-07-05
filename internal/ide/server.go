package ide

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// Allow connections from localhost for development
		return true
	},
}

// NewServer creates a new IDE server
func NewServer(config Config) *Server {
	if config.Port == 0 {
		config.Port = 8123
	}

	return &Server{
		config:      config,
		context:     &IDEContext{},
		connections: make(map[*websocket.Conn]bool),
		broadcast:   make(chan []byte),
		register:    make(chan *websocket.Conn),
		unregister:  make(chan *websocket.Conn),
	}
}

// Start starts the WebSocket server
func (s *Server) Start(ctx context.Context) error {
	if !s.config.Enable {
		return fmt.Errorf("IDE integration is disabled")
	}

	s.running = true

	// Start the hub
	go s.run()

	// Set up HTTP server
	http.HandleFunc("/ws", s.handleWebSocket)
	http.HandleFunc("/health", s.handleHealth)

	server := &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", s.config.Port),
		Handler: nil,
	}

	// Print handshake message for VS Code extension detection
	fmt.Printf("%s\n", HandshakeMessage)
	fmt.Printf("DevGru IDE server starting on ws://127.0.0.1:%d/ws\n", s.config.Port)

	// Start server in goroutine
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("IDE server error: %v", err)
		}
	}()

	// Wait for context cancellation
	<-ctx.Done()

	// Graceful shutdown
	s.running = false
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return server.Shutdown(shutdownCtx)
}

// run handles the main server loop
func (s *Server) run() {
	for s.running {
		select {
		case conn := <-s.register:
			s.connections[conn] = true

		case conn := <-s.unregister:
			if _, ok := s.connections[conn]; ok {
				delete(s.connections, conn)
				conn.Close()
			}

		case message := <-s.broadcast:
			for conn := range s.connections {
				select {
				case <-time.After(10 * time.Second):
					delete(s.connections, conn)
					conn.Close()
				default:
					if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
						delete(s.connections, conn)
						conn.Close()
					}
				}
			}
		}
	}
}

// handleWebSocket handles WebSocket connections
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	s.register <- conn

	// Handle messages from the extension
	go s.handleMessages(conn)
}

// handleHealth provides a health check endpoint
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"service": "devgru-ide",
		"port":    s.config.Port,
	})
}

// handleMessages processes incoming messages from VS Code extension
func (s *Server) handleMessages(conn *websocket.Conn) {
	defer func() {
		s.unregister <- conn
	}()

	for {
		_, messageBytes, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		var msg Message
		if err := json.Unmarshal(messageBytes, &msg); err != nil {
			log.Printf("Failed to parse message: %v", err)
			continue
		}

		s.processMessage(msg)
	}
}

// processMessage processes different types of messages from the extension
func (s *Server) processMessage(msg Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch msg.Type {
	case "selection":
		var selection SelectionMessage
		if data, _ := json.Marshal(msg.Data); data != nil {
			json.Unmarshal(data, &selection)
			s.context.Selection = &selection
			s.context.ActiveFile = selection.File
		}

	case "diagnostic":
		var diagnostic DiagnosticMessage
		if data, _ := json.Marshal(msg.Data); data != nil {
			json.Unmarshal(data, &diagnostic)
			s.context.Diagnostics = append(s.context.Diagnostics, diagnostic)
			if len(s.context.Diagnostics) > 10 {
				s.context.Diagnostics = s.context.Diagnostics[1:]
			}
		}

	case "fileChange":
		if file, ok := msg.Data["file"].(string); ok {
			s.context.ActiveFile = file
		}
		if s.context.Selection != nil && s.context.Selection.File != s.context.ActiveFile {
			s.context.Selection = nil
		}
	case "workspace":
		if root, ok := msg.Data["root"].(string); ok {
			s.context.WorkspaceRoot = root
		}
		if files, ok := msg.Data["open_files"].([]interface{}); ok {
			var openFiles []string
			for _, f := range files {
				if file, ok := f.(string); ok {
					openFiles = append(openFiles, file)
				}
			}
			s.context.OpenFiles = openFiles
		}

	default:
		log.Printf("❓ Unknown message type: %s", msg.Type)
	}
}

// GetContext returns the current IDE context
func (s *Server) GetContext() *IDEContext {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Return a copy to avoid race conditions
	ctx := &IDEContext{
		ActiveFile:    s.context.ActiveFile,
		WorkspaceRoot: s.context.WorkspaceRoot,
		OpenFiles:     make([]string, len(s.context.OpenFiles)),
	}
	copy(ctx.OpenFiles, s.context.OpenFiles)

	if s.context.Selection != nil {
		selection := *s.context.Selection
		ctx.Selection = &selection
	}

	ctx.Diagnostics = make([]DiagnosticMessage, len(s.context.Diagnostics))
	copy(ctx.Diagnostics, s.context.Diagnostics)

	return ctx
}

// SendDiff sends a diff to VS Code for display
func (s *Server) SendDiff(diff DiffResult) error {
	if !s.running {
		return fmt.Errorf("IDE server not running")
	}

	// Print diff markers for extension to detect
	fmt.Printf("%s\n", DiffStartMarker)
	fmt.Printf("%s\n", diff.Patch)
	fmt.Printf("%s\n", DiffEndMarker)

	// Also send via WebSocket
	message := Message{
		Type:      "diff",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"file":         diff.File,
			"patch":        diff.Patch,
			"orig_content": diff.OrigContent,
			"new_content":  diff.NewContent,
			"language":     diff.Language,
		},
	}

	messageBytes, err := json.Marshal(message)
	if err != nil {
		return err
	}

	select {
	case s.broadcast <- messageBytes:
		return nil
	case <-time.After(1 * time.Second):
		return fmt.Errorf("timeout sending diff")
	}
}

// IsConnected returns true if VS Code extension is connected
func (s *Server) IsConnected() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.connections) > 0
}
