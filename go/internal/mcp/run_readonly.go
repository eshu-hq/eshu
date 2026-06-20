package mcp

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// RunReadOnlyTool invokes a read-only MCP tool in-process without an HTTP round-trip.
//
// It is a thin exported seam over the unexported dispatchTool function, intended
// for the Ask Eshu engine and similar callers that need to invoke read capabilities
// programmatically without going through the network stack.
//
// Constraints:
//   - Only routes that resolveRoute recognises are supported; mutation tools are
//     not reachable through this function but callers should not rely on that as a
//     safety boundary — use an appropriately scoped handler.
//   - The handler must be safe for concurrent use; RunReadOnlyTool makes a single
//     synchronous ServeHTTP call per invocation.
//   - Scope and authentication are threaded via ctx and authHeader, exactly as they
//     are for the normal MCP transport path.
//
// Returns (envelope, isError, nil) on a successful dispatch. Returns (nil, false, err)
// when the tool name is unknown or the dispatch fails before a response is produced.
func RunReadOnlyTool(
	ctx context.Context,
	handler http.Handler,
	toolName string,
	args map[string]any,
	authHeader string,
	logger *slog.Logger,
) (*query.ResponseEnvelope, bool, error) {
	result, err := dispatchTool(ctx, handler, toolName, args, authHeader, logger)
	if err != nil {
		return nil, false, err
	}
	return result.Envelope, result.IsError, nil
}
