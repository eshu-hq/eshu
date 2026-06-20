package engine

import (
	"context"
	"io"
	"log/slog"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/mcp"
)

// callerAuthHeaderKey is the context key for a per-request caller auth header
// override. When set, it takes precedence over the baked-in authHeader so that
// a scoped-token caller's Authorization value is threaded through to every
// inner tool call dispatched by the engine, preserving the caller's grant
// bounds end-to-end and never widening scope to the shared admin key.
type callerAuthHeaderKey struct{}

// ContextWithCallerAuthHeader returns a child context carrying the given
// Authorization header value. The mcpRunner reads this value during Run so
// that inner tool calls inherit the caller's scoped token rather than the
// shared API key baked in at engine construction time.
//
// Callers MUST pass the exact Authorization header value as presented by the
// HTTP request (e.g. "Bearer <token>"). An empty string is accepted but has no
// effect: an empty override causes the runner to fall back to its baked-in
// authHeader.
func ContextWithCallerAuthHeader(ctx context.Context, authHeader string) context.Context {
	return context.WithValue(ctx, callerAuthHeaderKey{}, authHeader)
}

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
// authHeader carries the fallback bearer token used when the execution context
// does not carry a per-request caller override (see ContextWithCallerAuthHeader).
// For shared-token callers this is "Bearer <shared-api-key>". For scoped-token
// callers the per-request context override takes precedence, so inner tool calls
// always reflect the caller's own grant rather than the shared admin key.
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

// Run dispatches the named read-only tool call in-process and returns a
// RunResult describing the outcome.
//
// Auth header resolution: if ctx carries a non-empty caller auth header (set by
// ContextWithCallerAuthHeader), that value is used as the Authorization header
// for the inner tool call. Otherwise the baked-in authHeader is used. This
// ensures scoped-token callers never have their inner calls dispatched under the
// shared admin key.
//
// Error handling:
//   - A non-nil error is returned for transport or dispatch failures (unknown
//     tool name, handler panic recovery, parse failure). The caller should treat
//     these as hard failures.
//   - When the tool returns a canonical ResponseEnvelope (isError or not), the
//     envelope is preserved in RunResult.Envelope so its error truth becomes an
//     unsupported AnswerPacket downstream; see Runner interface doc.
//   - When the tool returns plain JSON (no canonical envelope), the decoded value
//     is preserved in RunResult.Value so the engine can feed it to the LLM.
func (r *mcpRunner) Run(ctx context.Context, toolName string, args map[string]any) (RunResult, error) {
	authHeader := r.authHeader
	if override, ok := ctx.Value(callerAuthHeaderKey{}).(string); ok && override != "" {
		authHeader = override
	}
	envelope, value, _, err := mcp.RunReadOnlyTool(ctx, r.handler, toolName, args, authHeader, r.logger) // isError is intentionally discarded — the envelope is returned regardless so its error truth becomes an unsupported AnswerPacket downstream; see Run doc.
	if err != nil {
		return RunResult{}, err
	}
	return RunResult{Envelope: envelope, Value: value}, nil
}
