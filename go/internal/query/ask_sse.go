package query

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

// acceptsSSE reports whether the request's Accept header indicates the caller
// wants a Server-Sent Events stream.
func acceptsSSE(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/event-stream")
}

// tokenEventPayload is the JSON shape for "event: token" SSE events.
// It carries only the incremental text delta; never raw provider frames.
type tokenEventPayload struct {
	Delta string `json:"delta"`
}

// handleAskSSE serves the SSE (text/event-stream) variant of POST /api/v0/ask.
//
// # Contract
//
// When the Asker supports streaming (AskStream does not return ErrNoStreaming),
// the handler drives AskStream and emits events live as they occur:
//
//   - one "token" event per assistant text delta: {delta: "..."}
//   - one "trace" event per completed tool call (bounded traceEntry JSON)
//   - one "answer" event carrying the final askResponse
//   - one "done" event with an empty payload
//   - on engine error, one "error" event with a bounded askUnavailableResponse
//
// When the adapter does not support streaming (ErrNoStreaming), the handler
// falls back to a synchronous Ask call and emits only "trace", "answer", and
// "done" events (no "token" events). The fallback path preserves backward
// compatibility with synchronous-only adapters.
//
// # Default-off
//
// If h.Asker is nil the handler writes the standard 503 unavailable JSON
// response before opening the event stream. The HTTP status code is 503 so
// callers can detect this before reading the body.
//
// # Leak safety
//
// Only bounded askResponse / traceEntry / askUnavailableResponse / tokenEventPayload
// values are emitted. Provider bodies, prompts, raw engine internals, and
// credentials are never written to the stream.
func (h *AskHandler) handleAskSSE(w http.ResponseWriter, r *http.Request) {
	// Default-off: respond with the standard 503 JSON before opening a stream.
	if h.Asker == nil {
		WriteJSON(w, http.StatusServiceUnavailable, askUnavailableResponse{
			State:  "unavailable",
			Reason: "ask is not enabled; set ESHU_ASK_ENABLED=true and configure an agent_reasoning provider profile",
		})
		return
	}

	var req askRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]string{
			"error":  "bad_request",
			"detail": "invalid JSON body",
		})
		return
	}

	if strings.TrimSpace(req.Question) == "" {
		WriteJSON(w, http.StatusBadRequest, map[string]string{
			"error":  "bad_request",
			"detail": "question is required and must not be empty",
		})
		return
	}

	// Verify the ResponseWriter supports flushing before committing SSE headers.
	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{
			"error":  "internal_error",
			"detail": "streaming not supported by this server configuration",
		})
		return
	}

	// Commit SSE headers — after this point every response byte goes on the stream.
	hdr := w.Header()
	hdr.Set("Content-Type", "text/event-stream")
	hdr.Set("Cache-Control", "no-cache")
	hdr.Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	logger := h.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// Try streaming path first. If the adapter does not support streaming,
	// fall back to the synchronous path (no token events).
	ans, err := h.Asker.AskStream(r, req.Question, func(ev AskStreamEvent) {
		switch ev.Kind {
		case "token":
			writeSSEEvent(w, "token", tokenEventPayload{Delta: ev.TextDelta})
			flusher.Flush()
		case "trace_entry":
			if ev.TraceEntry != nil {
				writeSSEEvent(w, "trace", traceEntry{
					Tool:       ev.TraceEntry.Tool,
					Supported:  ev.TraceEntry.Supported,
					TruthClass: string(ev.TraceEntry.TruthClass),
				})
				flusher.Flush()
			}
			// tool_call_started events are not forwarded to the SSE stream: they
			// carry only the tool name and call ID, which are already covered by the
			// subsequent trace_entry event. Forwarding them separately would require
			// a new event type visible to SSE clients with no additional truth value.
		}
	})

	if err != nil {
		if errors.Is(err, ErrNoStreaming) {
			// Adapter does not support streaming: fall back to synchronous Ask.
			h.handleAskSSESync(w, r, req, flusher, logger)
			return
		}
		logger.Warn("ask engine error (SSE)", "err_type", "engine_failure")
		writeSSEEvent(w, "error", askUnavailableResponse{
			State:  "unavailable",
			Reason: "ask engine encountered an error; see operator logs",
		})
		flusher.Flush()
		return
	}

	// Emit the full answer using the same mapping as the JSON path.
	resp := buildAskResponse(ans, req.Question, req.Format)
	writeSSEEvent(w, "answer", resp)
	flusher.Flush()

	// Signal end-of-stream.
	writeSSEEvent(w, "done", struct{}{})
	flusher.Flush()
}

// handleAskSSESync is the synchronous fallback for handleAskSSE when the
// adapter does not support streaming. It emits "trace", "answer", and "done"
// events (no "token" events).
func (h *AskHandler) handleAskSSESync(
	w http.ResponseWriter,
	r *http.Request,
	req askRequest,
	flusher http.Flusher,
	logger *slog.Logger,
) {
	ans, err := h.Asker.Ask(r, req.Question)
	if err != nil {
		logger.Warn("ask engine error (SSE sync)", "err_type", "engine_failure")
		writeSSEEvent(w, "error", askUnavailableResponse{
			State:  "unavailable",
			Reason: "ask engine encountered an error; see operator logs",
		})
		flusher.Flush()
		return
	}

	// Emit one trace event per trace entry (bounded fields only).
	// Args are omitted from trace events: they may contain query parameters
	// that were not explicitly authorised for streaming exposure. The full
	// trace including Args is present in the answer event's query_trace array.
	for _, t := range ans.Trace {
		writeSSEEvent(w, "trace", traceEntry{
			Tool:       t.Tool,
			Supported:  t.Supported,
			TruthClass: string(t.TruthClass),
		})
		flusher.Flush()
	}

	resp := buildAskResponse(ans, req.Question, req.Format)
	writeSSEEvent(w, "answer", resp)
	flusher.Flush()

	writeSSEEvent(w, "done", struct{}{})
	flusher.Flush()
}

// writeSSEEvent encodes v as JSON and writes one Server-Sent Event to w in the
// format:
//
//	event: <name>\n
//	data: <json>\n
//	\n
//
// Encoding errors produce a minimal error marker so the client can detect the
// failure without leaking internal state. Write errors are intentionally
// swallowed: after SSE headers are committed there is no mechanism to surface a
// transport error to the caller via status code, and panicking inside a
// streaming handler would terminate the connection abruptly.
func writeSSEEvent(w http.ResponseWriter, name string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		_, _ = fmt.Fprintf(w, "event: %s\ndata: {\"error\":\"encode_failure\"}\n\n", name)
		return
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, b)
}
