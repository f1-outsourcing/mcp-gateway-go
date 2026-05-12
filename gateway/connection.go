package gateway

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)


type connection struct {
	stdin io.Writer

	writeMu sync.Mutex

	pendingMu sync.Mutex
	pending   map[interface{}]chan json.RawMessage
}

type McpCommand struct {
	Command string
	Args   []string
	Env    []string
}

type GatewaySSEServer struct {
	*SSEServer
	command McpCommand
}

func  NewGatewaySSEServer(command McpCommand, sseServer *SSEServer) *GatewaySSEServer {
	return &GatewaySSEServer{
		SSEServer: sseServer,
		command:   command,
	}
}

func (g *GatewaySSEServer) Start(addr string) error {
	cmd := exec.Command(g.command.Command, g.command.Args...)
	cmd.Env = append(os.Environ(), g.command.Env...)

	cmd.Stderr = os.Stderr

	if err := g.InitStdioConn(cmd);err != nil {
		return err
	}

	return g.SSEServer.Start(addr)
}

func (s *GatewaySSEServer) InitStdioConn(cmd *exec.Cmd) error {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	s.SSEServer.conn = &connection{
		stdin:   stdin,
		pending: make(map[interface{}]chan json.RawMessage),
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start MCP process: %w", err)
	}

	go s.readLoop(stdout)

	go func() {
		err := cmd.Wait()
		log.Printf("MCP process exited: %v", err)
	}()

	return nil
}

func (s *GatewaySSEServer) readLoop(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)

	for scanner.Scan() {
		line := scanner.Bytes()

		var raw json.RawMessage
		raw = append(raw[:0], line...)

		var msg map[string]interface{}
		if err := json.Unmarshal(raw, &msg); err != nil {
			log.Printf("invalid MCP json: %s", string(raw))
			continue
		}

		id, hasID := msg["id"]

		if hasID {
			s.conn.pendingMu.Lock()

			ch, ok := s.conn.pending[id]
			if ok {
				ch <- raw
				close(ch)
				delete(s.conn.pending, id)
			}

			s.conn.pendingMu.Unlock()
		} else {
			log.Printf("notification: %s", string(raw))
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("stdout read error: %v", err)
	}
}

func (s *SSEServer) forwardToStdio(message json.RawMessage) (json.RawMessage, error) {
	var msg map[string]interface{}

	if err := json.Unmarshal(message, &msg); err != nil {
		return nil, err
	}

	id, hasID := msg["id"]

	s.conn.writeMu.Lock()
	defer s.conn.writeMu.Unlock()

	if !hasID {
		_, err := fmt.Fprintf(s.conn.stdin, "%s\n", message)
		return nil, err
	}

	respChan := make(chan json.RawMessage, 1)

	s.conn.pendingMu.Lock()
	s.conn.pending[id] = respChan
	s.conn.pendingMu.Unlock()

	if _, err := fmt.Fprintf(s.conn.stdin, "%s\n", message); err != nil {
		return nil, err
	}

	select {
	case resp := <-respChan:
		return resp, nil

	case <-time.After(60 * time.Second):
		s.conn.pendingMu.Lock()
		delete(s.conn.pending, id)
		s.conn.pendingMu.Unlock()

		return nil, fmt.Errorf("timeout waiting for MCP response")
	}
}

func (s *SSEServer) handleMessageToStdio(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeJSONRPCError(w, nil, mcp.INVALID_REQUEST, "Method not allowed")
		return
	}

	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		s.writeJSONRPCError(w, nil, mcp.INVALID_PARAMS, "Missing sessionId")
		return
	}
	sessionI, ok := s.sessions.Load(sessionID)
	if !ok {
		s.writeJSONRPCError(w, nil, mcp.INVALID_PARAMS, "Invalid session ID")
		return
	}
	session := sessionI.(*sseSession)

	var rawMessage json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&rawMessage); err != nil {
		s.writeJSONRPCError(w, nil, mcp.PARSE_ERROR, "Parse error")
		return
	}

	// Process message through MCPServer
	response, err := s.forwardToStdio(rawMessage)
	if err != nil {
		s.writeJSONRPCError(w, nil, mcp.INTERNAL_ERROR, fmt.Sprintf("MCP communication error: %v", err))
		return
	}

	// Only send response if there is one (not for notifications)
	if response != nil {
		eventData, _ := json.Marshal(response)

		// Queue the event for sending via SSE
		select {
		case session.eventQueue <- fmt.Sprintf("event: message\ndata: %s\n\n", eventData):
			// Event queued successfully
		case <-session.done:
			// Session is closed, don't try to queue
		default:
			// Queue is full, could log this
		}

		// Send HTTP response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(response)
	} else {
		// For notifications, just send 202 Accepted with no body
		w.WriteHeader(http.StatusAccepted)
	}
}

func (s *SSEServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.validateOAuth2Bearer(r) {
		http.Error(w, "Unauthorized: Invalid or missing Bearer token", http.StatusUnauthorized)
		return
	}
	path := r.URL.Path
	// Use exact path matching rather than Contains
	ssePath := s.CompleteSsePath()
	if ssePath != "" && path == ssePath {
		s.handleSSE(w, r)
		return
	}
	messagePath := s.CompleteMessagePath()
	if messagePath != "" && path == messagePath {
		s.handleMessageToStdio(w, r)
		return
	}

	http.NotFound(w, r)
}

func (s *SSEServer) validateOAuth2Bearer(r *http.Request) bool {
	if s.oAuth2Bearer == "" {
		return true
	}

	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return false
	}

	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return false
	}

	token := parts[1]
	return token == s.oAuth2Bearer
}

func WithOAuth2Bearer(token string) SSEOption {
	return func(s *SSEServer) {
		s.oAuth2Bearer = token
	}
}