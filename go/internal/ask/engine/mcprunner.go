package engine

import (
	"context"
	"io"
	"log/slog"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/mcp"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// mcpRunner is a Runner that dispatches tool calls in-process through the Eshu
// MCP/API handler using mcp.RunReadOnlyTool. It is the production wiring used
// by the Ask Eshu engine when the caller holds a scoped API token.
type mcpRunner struct {
	handler    http.Handler
	authHeader string
	logger     *slog.Logger
}

// NewMCPRunner returns a Runner backed by the given API handler.
//
// handler is the Eshu API mux. All reads are dispatched in-process: no network
// socket is opened and no external process is forked.
//
// authHeader carries the caller's scoped token exactly as it would appear in an
// HTTP Authorization header (e.g. "Bearer <token>"). The engine threads the
// caller's AuthContext via ctx; authHeader provides the token that the handler
// uses to enforce scope at the query layer.
//
// If logger is nil, a discard logger is used; the caller is not required to
// provide one.
func NewMCPRunner(handler http.Handler, authHeader string, logger *slog.Logger) Runner {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &mcpRunner{
		handler:    handler,
		authHeader: authHeader,
		logger:     logger,
	}
}

// Run dispatches the named read-only tool call in-process and returns the
// canonical ResponseEnvelope.
//
// Error handling:
//   - A non-nil error is returned for transport or dispatch failures (unknown
//     tool name, handler panic recovery, parse failure). The caller should treat
//     these as hard failures.
//   - When isError is true the envelope is still returned rather than converted
//     to a Go error. This preserves the truth envelope so the engine can wrap it
//     in a NewAnswerPacket call, which produces an unsupported AnswerPacket that
//     faithfully reflects the query surface's error verdict rather than silently
//     dropping it.
func (r *mcpRunner) Run(ctx context.Context, toolName string, args map[string]any) (*query.ResponseEnvelope, error) {
	envelope, _, err := mcp.RunReadOnlyTool(ctx, r.handler, toolName, args, r.authHeader, r.logger)
	if err != nil {
		return nil, err
	}
	return envelope, nil
}
