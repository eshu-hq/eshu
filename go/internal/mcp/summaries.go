package mcp

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// Text summaries are a convenience layer for human readers only. The structured
// content and the embedded resource block remain the canonical source of truth.
// Every summary built here MUST be deterministic given its input and MUST stay
// within maxSummaryLength so that a rich result never expands into an unbounded
// string.

// maxSummaryLength caps every generated text summary. Summaries are a human
// convenience, not a transport, so they are clamped rather than allowed to grow
// with the result payload.
const maxSummaryLength = 600

// maxLimitationLength caps a single embedded limitation/reason fragment so one
// oversized field cannot dominate or blow the summary budget.
const maxLimitationLength = 120

// summarizeToolText returns a deterministic, bounded, human-readable summary for
// an envelope-backed tool response. It reads only from the already-parsed
// envelope (no new queries) and never mutates the structured content. Tools
// without a specific summarizer fall back to the generic envelope summary.
func summarizeToolText(toolName string, envelope *query.ResponseEnvelope) string {
	if envelope == nil {
		return "Eshu query completed."
	}
	if envelope.Error != nil {
		return clampSummary(summarizeEnvelopeError(envelope.Error))
	}

	data, _ := envelope.Data.(map[string]any)

	var body string
	switch toolName {
	case "get_service_story", "get_workload_story", "get_repo_story":
		body = summarizeServiceStory(data)
	case "investigate_service":
		body = summarizeServiceInvestigation(data)
	case "get_incident_context":
		body = summarizeIncidentContext(data)
	case "build_evidence_citation_packet":
		body = summarizeCitationPacket(data)
	default:
		body = ""
	}

	prefix := summarizeTruthPrefix(envelope.Truth)
	if body == "" {
		// No tool-specific summarizer matched. Preserve the generic envelope
		// summary so unhandled tools still get a reasonable, bounded message,
		// while still surfacing truth + freshness when present.
		generic := summarizeEnvelope(envelope)
		return clampSummary(joinSummary(prefix, generic))
	}
	return clampSummary(joinSummary(prefix, body))
}

// summarizePlainToolText returns a deterministic, bounded summary for a tool
// whose response is a plain JSON payload rather than a canonical envelope (for
// example the status/readiness endpoints). Unhandled tools fall back to the
// generic plain-payload summary.
func summarizePlainToolText(toolName string, value any) string {
	data, _ := value.(map[string]any)

	var body string
	switch toolName {
	case "get_index_status":
		body = summarizeIndexStatus(data)
	case "get_ingester_status":
		body = summarizeIngesterStatus(data)
	case "list_ingesters":
		body = summarizeIngesterList(data)
	case "list_collectors":
		body = summarizeCollectorList(data)
	default:
		body = ""
	}

	if body == "" {
		return clampSummary(summarizePlainPayload(value))
	}
	return clampSummary(body)
}

// summarizeEnvelopeError renders an error envelope as truth, surfacing the
// machine-readable code alongside the human message so partial/failed responses
// never collapse into generic success text.
func summarizeEnvelopeError(errEnv *query.ErrorEnvelope) string {
	message := strings.TrimSpace(errEnv.Message)
	code := strings.TrimSpace(string(errEnv.Code))
	switch {
	case code != "" && message != "":
		return fmt.Sprintf("error %s: %s", code, message)
	case code != "":
		return fmt.Sprintf("error %s", code)
	case message != "":
		return message
	default:
		return "Eshu query returned an error."
	}
}

// summarizeTruthPrefix renders the truth level and freshness state (plus a
// bounded freshness detail when the state is not fresh) so every summary leads
// with how much to trust the result. Returns "" when no truth is present.
func summarizeTruthPrefix(truth *query.TruthEnvelope) string {
	if truth == nil {
		return ""
	}
	level := strings.TrimSpace(string(truth.Level))
	state := strings.TrimSpace(string(truth.Freshness.State))
	switch {
	case level != "" && state != "":
		prefix := fmt.Sprintf("%s/%s", level, state)
		if state != string(query.FreshnessFresh) {
			if detail := strings.TrimSpace(truth.Freshness.Detail); detail != "" {
				prefix += " (" + clampField(detail) + ")"
			}
		}
		return prefix
	case level != "":
		return level
	case state != "":
		return state
	default:
		return ""
	}
}

// joinSummary combines a truth prefix and a body with a deterministic separator,
// tolerating an empty prefix or body.
func joinSummary(prefix, body string) string {
	prefix = strings.TrimSpace(prefix)
	body = strings.TrimSpace(body)
	switch {
	case prefix == "" && body == "":
		return "Eshu query completed."
	case prefix == "":
		return body
	case body == "":
		return prefix
	default:
		return prefix + " — " + body
	}
}

// clampSummary bounds a full summary to maxSummaryLength, appending an ellipsis
// marker when truncation occurs so the reader knows the text was clipped.
func clampSummary(s string) string {
	if len(s) <= maxSummaryLength {
		return s
	}
	if maxSummaryLength <= 3 {
		return s[:maxSummaryLength]
	}
	return s[:maxSummaryLength-3] + "..."
}

// clampField bounds a single embedded fragment (a limitation, reason, or title)
// so one oversized field cannot dominate the summary.
func clampField(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= maxLimitationLength {
		return s
	}
	if maxLimitationLength <= 3 {
		return s[:maxLimitationLength]
	}
	return s[:maxLimitationLength-3] + "..."
}

// summarizeEnvelope is the generic, tool-agnostic envelope fallback used when no
// tool-specific summarizer matches. It surfaces an error message or a result
// count, defaulting to a neutral completion message.
func summarizeEnvelope(envelope *query.ResponseEnvelope) string {
	if envelope == nil {
		return "Eshu query completed."
	}
	if envelope.Error != nil {
		return summarizeEnvelopeError(envelope.Error)
	}
	if dataMap, ok := envelope.Data.(map[string]any); ok {
		if count, ok := dataMap["count"]; ok {
			return fmt.Sprintf("Returned %v result(s).", count)
		}
		if count, ok := dataMap["affected_count"]; ok {
			return fmt.Sprintf("Found %v affected result(s).", count)
		}
	}
	return "Eshu query completed."
}

// summarizePlainPayload is the generic, tool-agnostic fallback for plain JSON
// payloads. It reports an array length or a count-like field, defaulting to a
// neutral completion message.
func summarizePlainPayload(value any) string {
	switch typed := value.(type) {
	case []any:
		return fmt.Sprintf("Returned %d result(s).", len(typed))
	case map[string]any:
		for _, key := range []string{"count", "total", "total_findings", "total_reconciliations", "total_correlations", "total_identities", "total_attachments"} {
			if count, ok := typed[key]; ok {
				return fmt.Sprintf("Returned %v result(s).", count)
			}
		}
	}
	return "Eshu query completed."
}

// mcpMapField extracts a nested map[string]any from a JSON-parsed map, returning
// nil when the key is absent or not a map.
func mcpMapField(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	if nested, ok := m[key].(map[string]any); ok {
		return nested
	}
	return nil
}

// mcpSliceLen returns the length of a JSON-parsed slice value, tolerating both
// []any and []map[string]any. Non-slice values report length 0.
func mcpSliceLen(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	switch typed := m[key].(type) {
	case []any:
		return len(typed)
	case []map[string]any:
		return len(typed)
	default:
		return 0
	}
}

// mcpFirstStringInSlice returns the named string field of the first row in a
// slice value, or "" when the slice is empty or the field is absent. It is used
// to surface the single recommended next call deterministically.
func mcpFirstStringInSlice(m map[string]any, sliceKey, field string) string {
	if m == nil {
		return ""
	}
	switch typed := m[sliceKey].(type) {
	case []any:
		if len(typed) == 0 {
			return ""
		}
		if row, ok := typed[0].(map[string]any); ok {
			return query.StringVal(row, field)
		}
	case []map[string]any:
		if len(typed) == 0 {
			return ""
		}
		return query.StringVal(typed[0], field)
	}
	return ""
}
