package engine

import (
	"context"
	"io"
	"log/slog"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/mcp"
)

// callerAuthHeaderKey is the context key under which a per-request caller
// Authorization header is carried into the runner. Using an unexported type
// prevents collisions with other context values.
type callerAuthHeaderKey struct{}

// ContextWithCallerAuthHeader returns a child context carrying the caller's
// Authorization header (e.g. "Bearer <scoped-token>"). The mcpRunner reads it
// and uses it — in preference to the baked-in shared token — to authorize every
// in-process tool dispatch, so that a scoped caller's inner reads are re-checked
// against the scoped-route gate rather than running with the shared admin token.
//
// An empty header is ignored: the runner falls back to its baked-in token. This
// keeps the shared-token path (no caller header) working unchanged.
func ContextWithCallerAuthHeader(ctx context.Context, header string) context.Context {
	return context.WithValue(ctx, callerAuthHeaderKey{}, header)
}

// callerAuthHeaderFromContext returns the caller Authorization header carried by
// ctx, or "" when none is present.
func callerAuthHeaderFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(callerAuthHeaderKey{}).(string); ok {
		return v
	}
	return ""
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

// Run dispatches the named read-only tool call in-process and returns a
// RunResult describing the outcome.
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
	// Prefer the caller's per-request Authorization header (carried via ctx) so
	// a scoped caller's inner reads re-run the scoped-route gate under the
	// caller's identity. Fall back to the baked-in shared token when absent.
	authHeader := r.authHeader
	if caller := callerAuthHeaderFromContext(ctx); caller != "" {
		authHeader = caller
	}
	envelope, value, _, err := mcp.RunReadOnlyTool(ctx, r.handler, toolName, args, authHeader, r.logger) // isError is intentionally discarded — the envelope is returned regardless so its error truth becomes an unsupported AnswerPacket downstream; see Run doc.
	if err != nil {
		return RunResult{}, err
	}
	return RunResult{Envelope: envelope, Value: value}, nil
}
