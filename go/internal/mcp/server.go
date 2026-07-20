// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
	Content           []mcpContent `json:"content"`
	StructuredContent any          `json:"structuredContent,omitempty"`
	IsError           bool         `json:"isError,omitempty"`
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

// Server is the Go MCP server that dispatches tool calls to internal HTTP handlers.
type Server struct {
	handler http.Handler
	tools   []ToolDefinition
	logger  *slog.Logger
	mu      sync.Mutex

	// SSE session registry: sessionID -> session
	sessMu   sync.RWMutex
	sessions map[string]*sseSession

	// transportAuth wraps GET /sse and POST /mcp/message with the caller's
	// credential chain (see WithTransportAuth). nil means the HTTP transport
	// is unauthenticated -- the local stdio path never uses this field at all,
	// since Run() (stdio) never touches httpMux.
	transportAuth func(http.Handler) http.Handler
}

// NewServer creates an MCP server backed by the given HTTP handler.
// The handler should have all /api/v0/* query routes mounted.
func NewServer(handler http.Handler, logger *slog.Logger, opts ...ServerOption) *Server {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	}
	s := &Server{
		handler:  handler,
		tools:    ReadOnlyTools(),
		logger:   logger,
		sessions: make(map[string]*sseSession),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
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

	go func() { // #nosec G118 -- graceful-shutdown goroutine, not an HTTP handler goroutine; ReadHeaderTimeout is set on the server above
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

// Handler returns the full HTTP mux RunHTTP would serve -- GET /sse,
// POST /mcp/message, GET /health, and (via base) the shared runtime admin
// routes and /api/v0/* query passthrough -- without binding a real listener.
// It lets a caller (an embedding host, or a test exercising the real
// transport-auth composition end-to-end with httptest.NewServer) drive the
// transport surface directly instead of going through RunHTTP's blocking
// http.Server.ListenAndServe.
func (s *Server) Handler(base *http.ServeMux) http.Handler {
	return s.httpMux(base)
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

	// SSE transport endpoint and the JSON-RPC endpoint (standalone POST or
	// SSE-linked POST) both require the same credential chain as tools/call
	// when transport auth is configured (issue #5168); authenticatedTransportHandler
	// is a no-op wrap when s.transportAuth is nil.
	httpMux.HandleFunc("GET /sse", s.authenticatedTransportHandler("sse", s.handleSSE))
	httpMux.HandleFunc("POST /mcp/message", s.authenticatedTransportHandler("", s.handleHTTPMessage))

	// Mount the query API routes so the MCP service can also serve
	// direct HTTP queries (single deployment surface in EKS).
	httpMux.Handle("/api/", s.handler)

	return httpMux
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
				Result:  mcpToolErrorResult(err),
			}
		}
		if result.Envelope != nil {
			resourceText, _ := json.Marshal(result.Envelope)
			return &jsonrpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: mcpToolResult{
					Content: []mcpContent{
						{Type: "text", Text: summarizeToolText(params.Name, result.Envelope)},
						{
							Type: "resource",
							Resource: &mcpResource{
								URI:      "eshu://tool-result/envelope",
								MimeType: query.EnvelopeMIMEType,
								Text:     string(resourceText),
							},
						},
					},
					StructuredContent: result.Envelope,
					IsError:           result.IsError,
				},
			}
		}

		resultJSON, _ := json.Marshal(result.Value)
		return &jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: mcpToolResult{
				Content: []mcpContent{
					{Type: "text", Text: summarizePlainToolText(params.Name, result.Value)},
					{
						Type: "resource",
						Resource: &mcpResource{
							URI:      "eshu://tool-result/payload",
							MimeType: "application/json",
							Text:     string(resultJSON),
						},
					},
				},
				StructuredContent: result.Value,
				IsError:           result.IsError,
			},
		}

	case "ping":
		return &jsonrpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}

	default:
		return s.errorResponse(req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method))
	}
}

func mcpToolErrorResult(err error) mcpToolResult {
	result := mcpToolResult{
		Content: []mcpContent{{Type: "text", Text: err.Error()}},
		IsError: true,
	}
	if structured, ok := dispatchErrorStructuredContent(err); ok {
		structuredJSON, _ := json.Marshal(structured)
		result.StructuredContent = structured
		result.Content = append(result.Content, mcpContent{
			Type: "resource",
			Resource: &mcpResource{
				URI:      "eshu://tool-error/dispatch",
				MimeType: "application/json",
				Text:     string(structuredJSON),
			},
		})
	}
	return result
}

func (s *Server) errorResponse(id any, code int, msg string) *jsonrpcResponse {
	return &jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &jsonrpcError{Code: code, Message: msg},
	}
}
