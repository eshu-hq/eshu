// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/ask/facet"
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
	// PrimaryPacketIndex explicitly selects the packet that backs the published
	// response. Nil or an out-of-range index uses the ordinary first-supported
	// fallback. Packet and trace order always remain dispatch ordered.
	PrimaryPacketIndex *int
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
// Leak safety: token events carry only narration text that has passed the
// governed citation and publish-safety validator. Provider error bodies,
// credentials, prompts, raw LLM frames, and pre-validation provider deltas are
// never present.
type AskStreamEvent struct {
	// Kind is "token", "tool_call_started", or "trace_entry".
	Kind string
	// TextDelta is validated narration prose for Kind=="token".
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
	// AskStreamEvent as it occurs, and returns the final AskAnswer. Token
	// events must carry only validated narration prose, never raw provider
	// deltas. When the underlying engine or adapter does not support streaming,
	// implementations may return (zero, ErrNoStreaming) to cause the SSE
	// handler to fall back to the synchronous Ask path.
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
//	  "applied_facets":   object   // deterministic question-scoping metadata
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

// askAppliedFacets carries the deterministic detected-intent metadata that
// the pre-engine facet mapper found in the question. It is informational: the
// field records what the detector found in the question text, not what the
// agent actually filtered on — see the query_trace for the filters the agent
// passed to its tools. Callers can use this field to surface detected-scope
// chips in the UI without parsing the query trace.
type askAppliedFacets struct {
	// SourceTool is the canonical source_tool token detected in the question
	// (e.g. "helm", "terraform"). Empty when no canonical tool was detected.
	// This is detected intent, not a confirmed applied filter.
	SourceTool string `json:"source_tool,omitempty"`
	// Language is the programming-language name detected in the question
	// (e.g. "go", "python"). Empty when none was detected.
	// This is detected intent, not a confirmed applied filter.
	Language string `json:"language,omitempty"`
	// UnknownToolNote is a human-readable note when the question appeared to
	// name a specific tool that is not in the canonical vocabulary. Empty when
	// no unknown tool was detected.
	UnknownToolNote string `json:"unknown_tool_note,omitempty"`
}

// askResponse is the documented JSON response shape for POST /api/v0/ask.
type askResponse struct {
	AnswerProse string        `json:"answer_prose,omitempty"`
	Artifacts   []askArtifact `json:"artifacts,omitempty"`
	TruthClass  string        `json:"truth_class,omitempty"`
	// ResultRef references the primary packet's canonical API result. Result is
	// the packet's bounded embedded projection of that result.
	ResultRef       string                   `json:"result_ref,omitempty"`
	Result          any                      `json:"result,omitempty"`
	EvidenceHandles []evidenceCitationHandle `json:"evidence_handles,omitempty"`
	// CitationRef references the citation packet that hydrates the primary
	// packet's evidence handles. It is the packet-level citation coverage for the
	// answer prose when individual EvidenceHandles are not inlined, and is the
	// coverage anchor for derived, un-narrated prose (issue #3550).
	CitationRef   string            `json:"citation_ref,omitempty"`
	AppliedFacets *askAppliedFacets `json:"applied_facets,omitempty"`
	QueryTrace    []traceEntry      `json:"query_trace,omitempty"`
	Partial       bool              `json:"partial"`
	Limitations   []string          `json:"limitations,omitempty"`
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
	if !authContextAllowsPermissionFeature(r.Context(), permissionFeatureAskSearch) {
		writePermissionDeniedEnvelope(w, "ask_search.ask")
		return
	}
	if !authContextAllowsPermissionDataClasses(r.Context(), permissionDataClassesAskSearch...) {
		writePermissionDeniedEnvelope(w, "ask_search.ask")
		return
	}

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
	primarySupported := false
	var primarySummary string

	// Prose: only when the engine produced a narration.
	if ans.Narrated {
		resp.AnswerProse = ans.Prose
	}

	// Truth class, result, and evidence: taken from the primary packet.
	if primary, ok := primaryAskPacket(ans); ok {
		primarySupported = primary.Supported
		primarySummary = primary.Summary
		resp.TruthClass = string(primary.TruthClass)
		resp.ResultRef = strings.TrimSpace(primary.ResultRef)
		resp.Result = primary.Result
		if len(primary.EvidenceHandles) > 0 {
			resp.EvidenceHandles = primary.EvidenceHandles
		}
		// CitationRef is the packet-level citation coverage: it references the
		// citation packet that hydrates the handles. Surface it so derived,
		// un-narrated prose stays anchored to its citation packet (issue #3550).
		resp.CitationRef = strings.TrimSpace(primary.CitationRef)
	}

	applyAskRuntimeGuardrails(&resp, primarySupported)

	// Defense-in-depth: when narration produced no publishable prose but a
	// supported packet carries a deterministic Summary, surface that Summary as
	// derived prose so the answer is not silently empty (issue #3550).
	applyDerivedProseFallback(&resp, ans.Narrated, primarySupported, primarySummary)

	// Artifacts: when the answer has prose, validate the detected format and
	// include one artifact entry.
	detectedFormat := render.DetectFormat(question, format)
	if detectedFormat != render.FormatAuto && resp.AnswerProse != "" {
		artifact := render.Validate(detectedFormat, resp.AnswerProse)
		resp.Artifacts = []askArtifact{
			{
				Format:  string(artifact.Format),
				Content: artifact.Content,
				Issues:  artifact.Issues,
			},
		}
	}

	// Applied facets: record detected intent from the question and add honest
	// limitation notes. This is a pure recording step; see query_trace for the
	// filters the agent actually applied inside its tool calls.
	resp.AppliedFacets = buildAppliedFacets(question, &resp.Limitations)

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

// primaryAskPacket returns the explicitly selected packet when the engine
// identified one canonical answer. Otherwise it preserves the historical
// first-supported fallback. An invalid explicit index fails closed to the
// fallback so a malformed Asker cannot panic or suppress all packet truth.
func primaryAskPacket(ans AskAnswer) (AnswerPacket, bool) {
	if len(ans.Packets) == 0 {
		return AnswerPacket{}, false
	}
	if ans.PrimaryPacketIndex != nil {
		index := *ans.PrimaryPacketIndex
		if index >= 0 && index < len(ans.Packets) {
			return ans.Packets[index], true
		}
	}
	for _, packet := range ans.Packets {
		if packet.Supported {
			return packet, true
		}
	}
	return ans.Packets[0], true
}

// buildAppliedFacets runs DetectFacets on the question and returns a populated
// askAppliedFacets (or nil when the question has no detectable scope). It
// records detected-intent metadata and appends honest Limitation notes so the
// scoping is visible even to callers that ignore the applied_facets field.
// The field reflects what the pre-engine detector found; see the query_trace
// for the filters the agent actually applied inside its tool calls.
func buildAppliedFacets(question string, limitations *[]string) *askAppliedFacets {
	f := facet.DetectFacets(question)
	if f.SourceTool == "" && f.Language == "" && f.UnknownToolMention == "" {
		return nil
	}
	out := &askAppliedFacets{
		SourceTool: f.SourceTool,
		Language:   f.Language,
	}
	if f.SourceTool != "" {
		*limitations = appendAskLimitation(*limitations,
			"Detected a source_tool="+f.SourceTool+" intent in the question; see the query trace for the filters the agent actually applied.")
	}
	if f.Language != "" {
		*limitations = appendAskLimitation(*limitations,
			"Detected a language="+f.Language+" intent in the question; see the query trace for the filters the agent actually applied.")
	}
	if f.UnknownToolMention != "" {
		note := "'" + f.UnknownToolMention + "' is not a recognized Eshu source_tool; answered without a tool filter"
		out.UnknownToolNote = note
		*limitations = appendAskLimitation(*limitations, note)
	}
	return out
}
