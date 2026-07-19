// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// ServerOption configures optional Server construction-time behavior.
type ServerOption func(*Server)

// WithTransportAuth authenticates the MCP HTTP transport itself -- GET /sse
// session establishment and every POST /mcp/message method (initialize,
// tools/list, tools/call, ping) -- with middleware, rather than relying on
// tools/call's incidental internal re-dispatch through an authed handler
// (issue #5168). Callers should pass the SAME credential chain used for
// /api/v0/* routes (see cmd/mcp-server/wiring.go) so a credential that can
// call a tool can also establish a session and list the catalog, and a
// credential that cannot is refused uniformly. A nil middleware (the
// default) leaves the transport unauthenticated.
func WithTransportAuth(middleware func(http.Handler) http.Handler) ServerOption {
	return func(s *Server) {
		s.transportAuth = middleware
	}
}

// Bounded reason labels for eshu_dp_mcp_transport_auth_denied_total. Keep
// this set closed -- it is a metric label, not free text.
const (
	mcpAuthDenyReasonUnauthenticated = "unauthenticated"
	mcpAuthDenyReasonSessionMismatch = "session_principal_mismatch"
)

// knownMCPMethods bounds the "mcp_method" metric label extracted from a
// POST /mcp/message body before auth has necessarily succeeded. An unknown or
// unparsable method is labeled "other"/"unknown" rather than passed through
// raw, so a malformed or malicious body cannot explode label cardinality.
var knownMCPMethods = map[string]bool{
	"initialize":                true,
	"notifications/initialized": true,
	"tools/list":                true,
	"tools/call":                true,
	"ping":                      true,
}

// authenticatedTransportHandler wraps next with the server's transport-auth
// middleware (see WithTransportAuth) and records a labeled
// eshu_dp_mcp_transport_auth_denied_total on a 401/403 response, so an
// operator can see catalog-enumeration attempts against initialize/tools/list/
// tools/call/ping and GET /sse (issue #5168). staticMethod labels endpoints
// with no JSON-RPC body ("sse"); pass "" for POST /mcp/message to peek the
// JSON-RPC "method" field out of the request body without consuming it.
//
// When s.transportAuth is nil (no credential chain configured -- the
// escape-hatch or a deployment relying only on tools/call's own internal
// dispatch check), this is a plain pass-through: no peek, no wrapping, no
// metric.
func (s *Server) authenticatedTransportHandler(staticMethod string, next http.HandlerFunc) http.HandlerFunc {
	if s.transportAuth == nil {
		return next
	}
	wrapped := s.transportAuth(http.HandlerFunc(next))
	return func(w http.ResponseWriter, r *http.Request) {
		method := staticMethod
		if method == "" {
			method, r = peekMCPMethod(r)
		}
		rec := &authDenyStatusRecorder{ResponseWriter: w, status: http.StatusOK}
		wrapped.ServeHTTP(rec, r)
		if rec.status == http.StatusUnauthorized || rec.status == http.StatusForbidden {
			recordMCPTransportAuthDenied(r.Context(), method, mcpAuthDenyReasonUnauthenticated)
		}
	}
}

// mcpMethodPeek is the minimal JSON-RPC envelope needed to label a denied
// request by method before a real handler decodes it.
type mcpMethodPeek struct {
	Method string `json:"method"`
}

// peekMCPMethod reads r.Body fully, restores it (so the eventual handler can
// still decode the full request unaffected), and returns the bounded method
// label. It never truncates the body -- a partial restore would corrupt a
// legitimate large tools/call arguments payload.
func peekMCPMethod(r *http.Request) (string, *http.Request) {
	if r.Body == nil {
		return "unknown", r
	}
	raw, err := io.ReadAll(r.Body)
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(raw))
	if err != nil {
		return "unknown", r
	}
	var peek mcpMethodPeek
	if jsonErr := json.Unmarshal(raw, &peek); jsonErr != nil || peek.Method == "" {
		return "unknown", r
	}
	if !knownMCPMethods[peek.Method] {
		return "other", r
	}
	return peek.Method, r
}

// authDenyStatusRecorder captures the response status code so
// authenticatedTransportHandler can label the denied-request metric without
// buffering or altering the response body.
type authDenyStatusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *authDenyStatusRecorder) WriteHeader(status int) {
	if !w.wroteHeader {
		w.status = status
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *authDenyStatusRecorder) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.wroteHeader = true
	}
	return w.ResponseWriter.Write(b)
}

// Flush forwards to the underlying writer when it implements http.Flusher.
// GET /sse depends on this: without it, wrapping handleSSE in transport auth
// would hide the SSE handler's flushing behind a non-Flusher wrapper.
func (w *authDenyStatusRecorder) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// authPrincipalKey derives a stable per-credential identity string from the
// AuthContext a transport-auth middleware placed on ctx, so an SSE session
// can bind the credential presented at GET /sse and later reject a
// POST /mcp/message?sessionId=... from a different principal (issue #5168
// session-hijack requirement).
//
// It returns "" when no AuthContext is present -- either transport auth is
// not configured, or the shared-token/dev-mode path let the request through
// without resolving one. Callers MUST treat "" as "binding not enforced",
// never as a principal that must match another empty principal: both sides of
// an unauthenticated deployment are equally open, so there is nothing to bind.
func authPrincipalKey(ctx context.Context) string {
	auth, ok := query.AuthContextFromContext(ctx)
	if !ok {
		return ""
	}
	if auth.Mode == query.AuthModeShared {
		// The shared bearer token has exactly one principal: whoever holds
		// the one configured secret. There is no finer-grained identity to
		// bind, and only one valid token can exist at a time, so a constant
		// key correctly represents "the shared-token holder" for every
		// session opened this way.
		return "shared"
	}
	return string(auth.Mode) + "|" + auth.TenantID + "|" + auth.WorkspaceID + "|" + auth.SubjectIDHash
}
