package query

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/ask/render"
)

// AskAnswer is the handler-layer representation of an engine answer. It
// mirrors the fields the handler needs from ask/engine.Answer without
// importing that package, which would create an import cycle
// (ask/engine → query → ask/engine).
type AskAnswer struct {
	// Prose is the LLM-generated prose when Narrated is true.
	Prose string
	// Narrated is true when Prose contains a valid narration.
	Narrated bool
	// Packets are the evidence-backed AnswerPackets.
	Packets []AnswerPacket
	// Trace records every tool call in invocation order.
	Trace []AskTraceEntry
	// Partial is true when the answer is usable but incomplete.
	Partial bool
	// Limitations carries bounded human-readable caveats.
	Limitations []string
}

// AskTraceEntry is one tool-call entry in AskAnswer.Trace.
type AskTraceEntry struct {
	Tool       string
	Args       map[string]any
	Supported  bool
	TruthClass AnswerTruthClass
	Err        string
}

// AskStreamEvent is the handler-layer streaming event emitted by AskStream.
// It carries exactly one of TextDelta (for token events) or TraceEntry (for
// completed tool-call events). Callers must inspect Kind to determine which
// field is populated.
//
// Leak safety: only bounded assistant text deltas and tool identifiers are
// ever included. Provider error bodies, credentials, and raw LLM frames are
// never present.
type AskStreamEvent struct {
	// Kind is "token", "tool_call_started", or "trace_entry".
	Kind string
	// TextDelta is the incremental assistant text for Kind=="token".
	TextDelta string
	// ToolCallID is the provider call ID for Kind=="tool_call_started".
	ToolCallID string
	// ToolName is the tool name for Kind=="tool_call_started".
	ToolName string
	// TraceEntry is the completed tool-call result for Kind=="trace_entry".
	TraceEntry *AskTraceEntry
}

// Asker is the minimal interface AskHandler requires. Implementations convert
// an HTTP request + question into an AskAnswer using the engine. The interface
// lives in this package so cmd/api can implement it without creating a cycle:
// the implementation imports ask/engine; ask_handler.go does not.
type Asker interface {
	Ask(r *http.Request, question string) (AskAnswer, error)
	// AskStream drives a streaming Ask session, calling emit for each
	// AskStreamEvent as it occurs, and returns the final AskAnswer. When the
	// underlying engine or adapter does not support streaming, implementations
	// may return (zero, ErrNoStreaming) to cause the SSE handler to fall back
	// to the synchronous Ask path.
	AskStream(r *http.Request, question string, emit func(AskStreamEvent)) (AskAnswer, error)
}

// ErrNoStreaming is returned by Asker.AskStream implementations whose adapter
// does not support streaming. The SSE handler uses this signal to fall back to
// the synchronous Ask path rather than returning an error to the client.
var ErrNoStreaming = fmt.Errorf("ask: adapter does not support streaming")

// AskHandler handles POST /api/v0/ask.
//
// The handler is default-off: if no Asker is configured (nil), every request
// returns a bounded 503 JSON payload with state "unavailable". Callers MUST
// NOT rely on the 503 body shape being stable; it is informational only.
//
// When enabled, the handler decodes a JSON body containing at least a
// "question" field. An empty or missing question returns 400. It runs the
// engine and maps the resulting AskAnswer to the documented response shape:
//
//	{
//	  "answer_prose":     string   // LLM prose when narrated
//	  "artifacts":        []object // rendered format artifacts
//	  "truth_class":      string   // from primary packet
//	  "evidence_handles": []object // from primary packet
//	  "query_trace":      []object // tool-call trace
//	  "partial":          bool
//	  "limitations":      []string
//	}
//
// Leak safety: the handler never echoes provider error bodies, raw prompts,
// credential values, or engine internals. Engine errors are logged at WARN and
// map to a 503 with a static message.
type AskHandler struct {
	// Asker is the engine seam. nil means the handler is disabled.
	Asker  Asker
	Logger *slog.Logger
}

// askRequest is the decoded request body.
type askRequest struct {
	Question string `json:"question"`
	Format   string `json:"format,omitempty"`
}

// askArtifact is one element of the response artifacts array.
type askArtifact struct {
	Format  string   `json:"format"`
	Content string   `json:"content,omitempty"`
	Issues  []string `json:"issues,omitempty"`
}

// askResponse is the documented JSON response shape for POST /api/v0/ask.
type askResponse struct {
	AnswerProse     string                   `json:"answer_prose,omitempty"`
	Artifacts       []askArtifact            `json:"artifacts,omitempty"`
	TruthClass      string                   `json:"truth_class,omitempty"`
	EvidenceHandles []evidenceCitationHandle `json:"evidence_handles,omitempty"`
	QueryTrace      []traceEntry             `json:"query_trace,omitempty"`
	Partial         bool                     `json:"partial"`
	Limitations     []string                 `json:"limitations,omitempty"`
}

// traceEntry is the per-call representation in query_trace.
type traceEntry struct {
	Tool       string         `json:"tool"`
	Args       map[string]any `json:"args,omitempty"`
	Supported  bool           `json:"supported"`
	TruthClass string         `json:"truth_class,omitempty"`
	Err        string         `json:"err,omitempty"`
}

// askUnavailableResponse is returned when the handler is disabled or has no
// configured provider.
type askUnavailableResponse struct {
	State  string `json:"state"`
	Reason string `json:"reason"`
}

// Mount registers the ask route on mux.
func (h *AskHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v0/ask", h.handleAsk)
}

func (h *AskHandler) handleAsk(w http.ResponseWriter, r *http.Request) {
	// Content negotiation: SSE variant when the caller wants an event stream.
	if acceptsSSE(r) {
		h.handleAskSSE(w, r)
		return
	}

	// Default-off: no asker means the feature is disabled.
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

	ans, err := h.Asker.Ask(r, req.Question)
	if err != nil {
		logger := h.Logger
		if logger == nil {
			logger = slog.Default()
		}
		logger.Warn("ask engine error", "err_type", "engine_failure")
		WriteJSON(w, http.StatusServiceUnavailable, askUnavailableResponse{
			State:  "unavailable",
			Reason: "ask engine encountered an error; see operator logs",
		})
		return
	}

	resp := buildAskResponse(ans, req.Question, req.Format)
	WriteJSON(w, http.StatusOK, resp)
}

// buildAskResponse maps an AskAnswer to the wire response shape. It is a pure
// function, safe to call from tests without a real LLM.
func buildAskResponse(ans AskAnswer, question, format string) askResponse {
	resp := askResponse{
		Partial:     ans.Partial,
		Limitations: ans.Limitations,
	}

	// Prose: only when the engine produced a narration.
	if ans.Narrated {
		resp.AnswerProse = ans.Prose
	}

	// Truth class and evidence handles: taken from the primary packet.
	if len(ans.Packets) > 0 {
		primary := ans.Packets[0]
		for _, p := range ans.Packets {
			if p.Supported {
				primary = p
				break
			}
		}
		resp.TruthClass = string(primary.TruthClass)
		if len(primary.EvidenceHandles) > 0 {
			resp.EvidenceHandles = primary.EvidenceHandles
		}
	}

	// Artifacts: when the answer has prose, validate the detected format and
	// include one artifact entry.
	detectedFormat := render.DetectFormat(question, format)
	if detectedFormat != render.FormatAuto && ans.Narrated && ans.Prose != "" {
		artifact := render.Validate(detectedFormat, ans.Prose)
		resp.Artifacts = []askArtifact{
			{
				Format:  string(artifact.Format),
				Content: artifact.Content,
				Issues:  artifact.Issues,
			},
		}
	}

	// Query trace.
	if len(ans.Trace) > 0 {
		resp.QueryTrace = make([]traceEntry, len(ans.Trace))
		for i, t := range ans.Trace {
			resp.QueryTrace[i] = traceEntry{
				Tool:       t.Tool,
				Args:       t.Args,
				Supported:  t.Supported,
				TruthClass: string(t.TruthClass),
				Err:        t.Err,
			}
		}
	}

	return resp
}
