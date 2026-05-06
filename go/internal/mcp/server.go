package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// JSON-RPC 2.0 message types

type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      any           `json:"id,omitempty"`
	Result  any           `json:"result,omitempty"`
	Error   *jsonrpcError `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// MCP-specific types

type mcpInitializeResult struct {
	ProtocolVersion string          `json:"protocolVersion"`
	Capabilities    mcpCapabilities `json:"capabilities"`
	ServerInfo      mcpServerInfo   `json:"serverInfo"`
}

type mcpCapabilities struct {
	Tools *mcpToolsCap `json:"tools,omitempty"`
}

type mcpToolsCap struct {
	ListChanged bool `json:"listChanged"`
}

type mcpServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type mcpToolsListResult struct {
	Tools []ToolDefinition `json:"tools"`
}

type mcpToolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type mcpToolResult struct {
	Content []mcpContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

type mcpContent struct {
	Type     string       `json:"type"`
	Text     string       `json:"text,omitempty"`
	Resource *mcpResource `json:"resource,omitempty"`
}

type mcpResource struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
}

// sseSession holds the response channel for one SSE client.
type sseSession struct {
	ch chan []byte
}

// Server is the Go MCP server that dispatches tool calls to internal HTTP handlers.
type Server struct {
	handler http.Handler
	tools   []ToolDefinition
	logger  *slog.Logger
	mu      sync.Mutex

	// SSE session registry: sessionID -> session
	sessMu   sync.RWMutex
	sessions map[string]*sseSession
}

// NewServer creates an MCP server backed by the given HTTP handler.
// The handler should have all /api/v0/* query routes mounted.
func NewServer(handler http.Handler, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	}
	return &Server{
		handler:  handler,
		tools:    ReadOnlyTools(),
		logger:   logger,
		sessions: make(map[string]*sseSession),
	}
}

// RunHTTP starts the MCP server as an HTTP service listening on addr.
// It exposes:
//   - GET  /sse          — SSE transport (sends endpoint event, then keepalives)
//   - POST /mcp/message  — JSON-RPC endpoint (works standalone or with SSE session)
//   - GET  /health       — k8s probes
//   - shared runtime admin routes from the provided base mux
//   - /api/v0/*          — query API passthrough
//
// Blocks until ctx is cancelled.
func (s *Server) RunHTTP(ctx context.Context, addr string, base *http.ServeMux) error {
	httpMux := s.httpMux(base)

	srv := &http.Server{
		Addr:              addr,
		Handler:           httpMux,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      0, // disable for SSE long-lived connections
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	s.logger.Info("mcp server started", "transport", "http+sse", "addr", addr, "tools", len(s.tools))
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("listen: %w", err)
	}
	return nil
}

func (s *Server) httpMux(base *http.ServeMux) *http.ServeMux {
	httpMux := base
	if httpMux == nil {
		httpMux = http.NewServeMux()
	}

	// Health probe for MCP transport liveness.
	httpMux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// SSE transport endpoint.
	httpMux.HandleFunc("GET /sse", s.handleSSE)

	// MCP JSON-RPC endpoint (supports both standalone POST and SSE-linked POST).
	httpMux.HandleFunc("POST /mcp/message", s.handleHTTPMessage)

	// Mount the query API routes so the MCP service can also serve
	// direct HTTP queries (single deployment surface in EKS).
	httpMux.Handle("/api/", s.handler)

	return httpMux
}

// handleSSE establishes an SSE connection. It sends an `endpoint` event telling
// the client where to POST JSON-RPC messages, then streams keepalive events
// and any responses for the session.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Create a session for this SSE connection.
	sessionID := fmt.Sprintf("sess_%d", time.Now().UnixNano())
	sess := &sseSession{ch: make(chan []byte, 64)}

	s.sessMu.Lock()
	s.sessions[sessionID] = sess
	s.sessMu.Unlock()

	defer func() {
		s.sessMu.Lock()
		delete(s.sessions, sessionID)
		s.sessMu.Unlock()
		close(sess.ch)
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering

	// Send the endpoint event per MCP SSE spec.
	// The client uses this URL to POST JSON-RPC requests.
	_, _ = fmt.Fprintf(w, "event: endpoint\ndata: /mcp/message?sessionId=%s\n\n", sessionID)
	flusher.Flush()

	s.logger.Info("sse session started", "session_id", sessionID)

	// Keepalive ticker.
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			s.logger.Info("sse session closed", "session_id", sessionID)
			return
		case msg, ok := <-sess.ch:
			if !ok {
				return
			}
			_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg)
			flusher.Flush()
		case <-ticker.C:
			_, _ = fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// handleHTTPMessage handles POST /mcp/message. If a sessionId query param is
// present, the response is sent via the SSE stream. Otherwise, the response
// is returned directly in the HTTP response body.
func (s *Server) handleHTTPMessage(w http.ResponseWriter, r *http.Request) {
	var req jsonrpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(jsonrpcResponse{
			JSONRPC: "2.0",
			Error:   &jsonrpcError{Code: -32700, Message: "parse error"},
		})
		return
	}

	resp := s.handleMessage(r.Context(), &req, r.Header.Get("Authorization"))

	// Check for an SSE session.
	sessionID := r.URL.Query().Get("sessionId")
	if sessionID != "" {
		s.sessMu.RLock()
		sess, ok := s.sessions[sessionID]
		s.sessMu.RUnlock()

		if ok && resp != nil {
			encoded, err := json.Marshal(resp)
			if err == nil {
				select {
				case sess.ch <- encoded:
				default:
					s.logger.Warn("sse session buffer full, dropping message", "session_id", sessionID)
				}
			}
			// For SSE-linked requests, return 202 Accepted (response sent via SSE).
			w.WriteHeader(http.StatusAccepted)
			return
		}
	}

	// Standalone POST mode — return response directly.
	w.Header().Set("Content-Type", "application/json")
	if resp == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// Run starts the stdio JSON-RPC transport. It reads from stdin and writes to stdout.
// Blocks until ctx is cancelled or stdin is closed.
func (s *Server) Run(ctx context.Context) error {
	reader := bufio.NewReader(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)

	s.logger.Info("mcp server started", "transport", "stdio", "tools", len(s.tools))

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("read stdin: %w", err)
		}

		if len(line) == 0 || string(line) == "\n" {
			continue
		}

		var req jsonrpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			s.logger.Warn("invalid json-rpc", "error", err)
			continue
		}

		resp := s.handleMessage(ctx, &req, "")
		if resp == nil {
			continue // notification, no response needed
		}

		s.mu.Lock()
		if err := encoder.Encode(resp); err != nil {
			s.logger.Error("write response", "error", err)
		}
		s.mu.Unlock()
	}
}

func (s *Server) handleMessage(ctx context.Context, req *jsonrpcRequest, authHeader string) *jsonrpcResponse {
	switch req.Method {
	case "initialize":
		return &jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: mcpInitializeResult{
				ProtocolVersion: "2024-11-05",
				Capabilities: mcpCapabilities{
					Tools: &mcpToolsCap{ListChanged: false},
				},
				ServerInfo: mcpServerInfo{
					Name:    "eshu-mcp-server",
					Version: buildinfo.AppVersion(),
				},
			},
		}

	case "notifications/initialized":
		return nil // notification, no response

	case "tools/list":
		return &jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  mcpToolsListResult{Tools: s.tools},
		}

	case "tools/call":
		var params mcpToolCallParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.errorResponse(req.ID, -32602, "invalid params")
		}
		result, err := dispatchTool(ctx, s.handler, params.Name, params.Arguments, authHeader, s.logger)
		if err != nil {
			return &jsonrpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: mcpToolResult{
					Content: []mcpContent{{Type: "text", Text: err.Error()}},
					IsError: true,
				},
			}
		}
		if result.Envelope != nil {
			resourceText, _ := json.Marshal(result.Envelope)
			return &jsonrpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: mcpToolResult{
					Content: []mcpContent{
						{Type: "text", Text: summarizeEnvelope(result.Envelope)},
						{
							Type: "resource",
							Resource: &mcpResource{
								URI:      "eshu://tool-result/envelope",
								MimeType: query.EnvelopeMIMEType,
								Text:     string(resourceText),
							},
						},
					},
					IsError: result.IsError,
				},
			}
		}

		resultJSON, _ := json.Marshal(result.Value)
		return &jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: mcpToolResult{
				Content: []mcpContent{{Type: "text", Text: string(resultJSON)}},
			},
		}

	case "ping":
		return &jsonrpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}

	default:
		return s.errorResponse(req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method))
	}
}

func summarizeEnvelope(envelope *query.ResponseEnvelope) string {
	if envelope == nil {
		return "Eshu query completed."
	}
	if envelope.Error != nil {
		return envelope.Error.Message
	}
	if dataMap, ok := envelope.Data.(map[string]any); ok {
		if count, ok := dataMap["count"]; ok {
			return fmt.Sprintf("Returned %v result(s).", count)
		}
		if count, ok := dataMap["affected_count"]; ok {
			return fmt.Sprintf("Found %v affected result(s).", count)
		}
	}
	return "Eshu query completed."
}

func (s *Server) errorResponse(id any, code int, msg string) *jsonrpcResponse {
	return &jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &jsonrpcError{Code: code, Message: msg},
	}
}
