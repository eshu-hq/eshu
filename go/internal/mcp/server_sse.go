// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/mcp/router"
)

// sseSession holds the response channel for one SSE client. principal
// identifies the credential that opened the session (see authPrincipalKey);
// it is empty when transport auth is not configured, in which case session
// binding is not enforced (see handleHTTPMessage).
//
// mu/closed guard the channel lifecycle: handleSSE's teardown closes the
// channel exactly once (shutdown), and handleHTTPMessage delivers through
// send, which checks closed and does the channel send under the same lock.
// This makes the "is it still open?" check and the send atomic with respect
// to close, so a POST /mcp/message that runs a slow tools/call while the SSE
// client disconnects can never send on a closed channel (issue #5168 review
// P1: the session-map lookup now happens before dispatch, widening the
// close-vs-send window from microseconds to a full dispatch).
type sseSession struct {
	ch        chan []byte
	principal string
	// surface is the tool surface the client chose with GET /sse?surface=. It
	// is empty when the client chose none, so the process surface applies. A
	// POST that names its own surface still overrides it (#8758).
	surface router.Surface

	mu     sync.Mutex
	closed bool
}

// send delivers msg to the SSE stream. It returns false when the session is
// already closed (client disconnected) or its buffer is full, so the caller
// can log a drop rather than block or panic. The non-blocking select keeps
// the lock hold short: it never waits for a reader while holding mu.
func (sess *sseSession) send(msg []byte) bool {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.closed {
		return false
	}
	select {
	case sess.ch <- msg:
		return true
	default:
		return false
	}
}

// shutdown marks the session closed and closes the channel exactly once. It
// is safe to call concurrently with send: both take mu, so an in-flight send
// completes before shutdown closes the channel, and any later send observes
// closed and skips the channel entirely.
func (sess *sseSession) shutdown() {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.closed {
		return
	}
	sess.closed = true
	close(sess.ch)
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

	// A surface query parameter picks the tool surface for every message of
	// this SSE session. Reject an unknown value before a session exists.
	surface, err := parseSurfaceParam(r)
	if err != nil {
		writeSurfaceError(w, err)
		return
	}

	// Create a session for this SSE connection, binding it to the credential
	// that opened it (empty when transport auth is not configured -- see
	// authPrincipalKey and handleHTTPMessage's session-principal check).
	sessionID := fmt.Sprintf("sess_%d", time.Now().UnixNano())
	sess := &sseSession{ch: make(chan []byte, 64), principal: authPrincipalKey(r.Context()), surface: surface}

	s.sessMu.Lock()
	s.sessions[sessionID] = sess
	s.sessMu.Unlock()

	defer func() {
		s.sessMu.Lock()
		delete(s.sessions, sessionID)
		s.sessMu.Unlock()
		// shutdown (not a bare close) so a concurrent handleHTTPMessage send
		// on this session cannot race the close into a panic (issue #5168).
		sess.shutdown()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering

	// Send the endpoint event per MCP SSE spec.
	// The client uses this URL to POST JSON-RPC requests.
	// A chosen surface rides in the endpoint URL so every POST the client
	// follows carries it; ParseSurface canonicalizes it, so it is URL-safe.
	endpoint := "/mcp/message?sessionId=" + sessionID
	if surface != "" {
		endpoint += "&surface=" + string(surface)
	}
	_, _ = fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", endpoint)
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
//
// When sessionId names a live session, the request's resolved credential
// (via authPrincipalKey) must match the credential that opened the session
// (issue #5168 session-hijack requirement): a different principal is rejected
// with 403 before the request body is even decoded, so it cannot smuggle a
// message into another principal's SSE stream. Sessions opened without
// transport auth configured carry an empty principal and are never bound --
// both sides of an unauthenticated deployment are equally open, so there is
// nothing meaningful to bind.
//
// The captured sess pointer is reused for delivery after dispatch; sess.send
// tolerates the client disconnecting mid-dispatch (returns false, message
// dropped) rather than panicking on a closed channel.
func (s *Server) handleHTTPMessage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	var sess *sseSession
	if sessionID != "" {
		s.sessMu.RLock()
		sess = s.sessions[sessionID]
		s.sessMu.RUnlock()
	}

	if sess != nil && sess.principal != "" {
		if reqPrincipal := authPrincipalKey(r.Context()); reqPrincipal != sess.principal {
			recordMCPTransportAuthDenied(r.Context(), "mcp_message", mcpAuthDenyReasonSessionMismatch)
			// Tell authenticatedTransportHandler this 403 is already classified,
			// so its status recorder does not double-count it as a generic
			// unauthenticated denial (issue #5168 review P2).
			markTransportDenyClassified(r.Context())
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(jsonrpcResponse{
				JSONRPC: "2.0",
				Error:   &jsonrpcError{Code: -32001, Message: "credential does not match the session owner"},
			})
			return
		}
	}

	// A surface query parameter overrides the process tool surface for this
	// request; without one, an SSE-linked request uses its session's surface.
	// An unknown value is rejected before the body is read (#8758).
	ctx := r.Context()
	surface, err := parseSurfaceParam(r)
	if err != nil {
		writeSurfaceError(w, err)
		return
	}
	if surface == "" && sess != nil {
		surface = sess.surface
	}
	if surface != "" {
		ctx = withSurfaceOverride(ctx, surface)
	}

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

	resp := s.handleMessage(ctx, &req, r.Header.Get("Authorization"))

	if sess != nil {
		if resp != nil {
			encoded, err := json.Marshal(resp)
			if err == nil && !sess.send(encoded) {
				s.logger.Warn("sse session buffer full or closed, dropping message", "session_id", sessionID)
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

// parseSurfaceParam reads the surface query parameter. It returns "" when the
// parameter is absent or empty, so the caller keeps the process surface, and
// an error naming the allowed values when the value is unknown.
func parseSurfaceParam(r *http.Request) (router.Surface, error) {
	raw := r.URL.Query().Get("surface")
	if raw == "" {
		return "", nil
	}
	return router.ParseSurface(raw)
}

// writeSurfaceError answers an unknown surface value with a 400 JSON-RPC
// error whose message lists the allowed values.
func writeSurfaceError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(jsonrpcResponse{
		JSONRPC: "2.0",
		Error:   &jsonrpcError{Code: -32602, Message: err.Error()},
	})
}
