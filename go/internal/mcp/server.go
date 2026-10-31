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
	codedivergencetools "github.com/eshu-hq/eshu/go/internal/mcp/code/divergence"
	resultshape "github.com/eshu-hq/eshu/go/internal/mcp/result/shape"
	"github.com/eshu-hq/eshu/go/internal/mcp/router"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
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
	Content           []mcpContent      `json:"content"`
	StructuredContent any               `json:"structuredContent,omitempty"`
	IsError           bool              `json:"isError,omitempty"`
	Meta              map[string]string `json:"_meta,omitempty"`
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
	// surface selects what tools/list advertises from tools; the zero value
	// is router.SurfaceFull (see WithToolSurface).
	surface router.Surface
	// shape is the process default tools/call result shape; the zero value is
	// resultshape.Dual (see WithResultShape).
	shape  resultshape.Mode
	logger *slog.Logger
	mu     sync.Mutex

	// SSE session registry: sessionID -> session
	sessMu   sync.RWMutex
	sessions map[string]*sseSession

	// transportAuth wraps GET /sse and POST /mcp/message with the caller's
	// credential chain (see WithTransportAuth). nil means the HTTP transport
	// is unauthenticated -- the local stdio path never uses this field at all,
	// since Run() (stdio) never touches httpMux.
	transportAuth func(http.Handler) http.Handler

	router router.Host // serves find_tool and call_tool (#8757); ranker set by WithRouterRanker (#8771)
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
	s.router.Prepare(s.tools, s.logger) // after every option: the ranker indexes the final list (#8771)
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

	s.logger.Info("mcp server started", "transport", "http+sse", "addr", addr, "tools", len(s.tools), "tool_surface", string(s.processSurface()))
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

	s.logger.Info("mcp server started", "transport", "stdio", "tools", len(s.tools), "tool_surface", string(s.processSurface()))

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
		requested := requestedProtocolVersion(req.Params)
		negotiated := negotiateProtocolVersion(requested, supportedProtocolVersions)
		if requested != negotiated {
			s.logger.Info("mcp initialize requested an unsupported protocol version, answering with the server version",
				"requested_protocol_version", truncateForLog(requested), "negotiated_protocol_version", negotiated)
		}
		return &jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: mcpInitializeResult{
				ProtocolVersion: negotiated,
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
			Result:  mcpToolsListResult{Tools: s.advertisedTools(ctx)},
		}

	case "tools/call":
		var params mcpToolCallParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.errorResponse(req.ID, -32602, "invalid params")
		}
		return &jsonrpcResponse{JSONRPC: "2.0", ID: req.ID, Result: s.callTool(ctx, params, authHeader)}

	case "ping":
		return &jsonrpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}

	default:
		return s.errorResponse(req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method))
	}
}

// callTool answers one tools/call. find_tool and call_tool go to the router
// (#8757), which runs a call_tool target through the same run as a direct
// call, so a routed result is byte-identical to a direct one.
func (s *Server) callTool(ctx context.Context, params mcpToolCallParams, authHeader string) any {
	mode := s.shapeFor(ctx)
	ctx = resultshape.WithOverride(ctx, mode)
	run := func(ctx context.Context, name string, args map[string]any) (any, error) {
		result, err := dispatchTool(ctx, s.handler, name, args, authHeader, s.logger)
		if err != nil {
			return mcpToolErrorResultForCall(mode, name, err, s.logger, defaultToolResponseByteBudget), nil
		}
		return renderToolResult(name, result), nil
	}
	if !router.Intercepts(params.Name) {
		result, _ := run(ctx, params.Name, params.Arguments)
		return result
	}
	newConfig := func() router.Config {
		return router.Config{Registry: ReadOnlyTools(), Resolve: resolveRoute, Logger: s.logger}
	}
	req := router.Request{Visible: s.tools, Session: authPrincipalKey(ctx), Dispatch: run}
	return s.router.Serve(ctx, newConfig, req, params.Name, params.Arguments)
}

func (s *Server) errorResponse(id any, code int, msg string) *jsonrpcResponse {
	return &jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &jsonrpcError{Code: code, Message: msg},
	}
}

// WithQueryProfile filters the server's tools/list surface for the active
// query profile. Capabilities the matrix marks unsupported on production
// (ProductionMax nil, e.g. code_divergence.findings in
// go/internal/query/contract/capability_matrix.go) stay callable-gated on
// every profile, but a production-profile server no longer advertises tools
// that cannot succeed there (#7726). The filter runs once at construction,
// so the tools/list handler and the startup tools count observe the filtered
// surface. Any profile other than production, including the empty profile
// when this option is absent, keeps the full ReadOnlyTools surface.
// The option filters s.tools at application time, so it must be applied
// after any ServerOption that mutates s.tools; a later option that appends
// or replaces the tool list would otherwise re-advertise hidden tools.
func WithQueryProfile(profile querycontract.QueryProfile) ServerOption {
	return func(s *Server) {
		s.tools = filterToolsForProfile(s.tools, profile)
	}
}

// filterToolsForProfile drops production-unsupported tools when profile is
// the production query profile and returns tools unchanged otherwise. It
// builds a fresh slice so callers never observe a truncated backing array.
func filterToolsForProfile(tools []ToolDefinition, profile querycontract.QueryProfile) []ToolDefinition {
	if querycontract.NormalizeQueryProfile(string(profile)) != querycontract.ProfileProduction {
		return tools
	}
	hidden := productionHiddenToolNames()
	kept := make([]ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		if !hidden[tool.Name] {
			kept = append(kept, tool)
		}
	}
	return kept
}

// productionHiddenToolNames is the tools/list hide set for the production
// query profile. It derives from the divergence family's own registration,
// so a future family member hides automatically, plus compare_code_paths,
// whose call_graph.compare_code_paths matrix row is production-unsupported
// until remote validation lands (#7933); the matrix-parity test locks the
// set to those capability-matrix rows instead of a second copy here. The
// callable gate stays: hidden tools remain call-gated on every profile.
func productionHiddenToolNames() map[string]bool {
	family := codedivergencetools.Tools()
	hidden := make(map[string]bool, len(family)+1)
	for _, tool := range family {
		hidden[tool.Name] = true
	}
	hidden["compare_code_paths"] = true
	return hidden
}

// WithToolSurface sets the tool surface tools/list advertises (#8758). It
// changes advertisement only: tools/call dispatches every registered tool
// whatever the surface. Unlike WithQueryProfile, whose hidden tools stay
// call-gated, a surface never gates a call. The surface is applied to s.tools
// when tools/list is served, so the result always derives from the
// profile-filtered list and hidden tools stay hidden whatever order the
// options are given in. router.SurfaceFull, the default,
// serves s.tools unchanged. The option itself runs once at construction and
// only records the value.
func WithToolSurface(surface router.Surface) ServerOption {
	return func(s *Server) {
		s.surface = surface
	}
}

// surfaceContextKey carries a per-request or per-SSE-session surface override from the HTTP
// transport (the surface query parameter on POST /mcp/message) to
// handleMessage without changing its signature.
type surfaceContextKey struct{}

// withSurfaceOverride returns ctx carrying surface as the override for the
// request being served.
func withSurfaceOverride(ctx context.Context, surface router.Surface) context.Context {
	return context.WithValue(ctx, surfaceContextKey{}, surface)
}

// processSurface returns the surface set by WithToolSurface, or
// router.SurfaceFull when the option is absent.
func (s *Server) processSurface() router.Surface {
	if s.surface == "" {
		return router.SurfaceFull
	}
	return s.surface
}

// advertisedTools returns the tools/list payload for this request: the
// override when ctx carries one, otherwise the process
// surface. The registry s.tools is never mutated.
func (s *Server) advertisedTools(ctx context.Context) []ToolDefinition {
	surface := s.processSurface()
	if override, ok := ctx.Value(surfaceContextKey{}).(router.Surface); ok {
		surface = override
	}
	return router.Advertise(s.tools, surface)
}

// ToolSurfaceSummary reports the process surface, how many tools its
// tools/list advertises, and the size in bytes of the marshaled tools/list
// result. Serving entrypoints log it once at startup so an operator can
// confirm which surface a deployment serves and what it costs a client.
func (s *Server) ToolSurfaceSummary() (surface router.Surface, count int, bytes int) {
	surface = s.processSurface()
	tools := router.Advertise(s.tools, surface)
	raw, err := json.Marshal(mcpToolsListResult{Tools: tools})
	if err != nil {
		return surface, len(tools), 0
	}
	return surface, len(tools), len(raw)
}
