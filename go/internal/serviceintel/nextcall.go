package serviceintel

import "strings"

// nextCallsToMaps converts typed next calls into the evidence-citation
// recommended_next_calls map shape carried by AnswerPacket, so report sections
// stay wire-compatible with every other answer surface. Empty calls are skipped.
func nextCallsToMaps(calls []NextCall) []map[string]any {
	if len(calls) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(calls))
	for _, call := range calls {
		if nextCallEmpty(call) {
			continue
		}
		entry := map[string]any{}
		if call.Tool != "" {
			entry["tool"] = call.Tool
		}
		if call.Route != "" {
			entry["route"] = call.Route
		}
		if call.Playbook != "" {
			entry["playbook"] = call.Playbook
		}
		if call.Reason != "" {
			entry["reason"] = call.Reason
		}
		out = append(out, entry)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// nextCallsFromMaps parses recommended_next_calls maps back into typed next
// calls for report-level aggregation. It tolerates the freshness-derived calls
// the AnswerPacket builder may inject, which carry the same tool/route/reason
// keys.
func nextCallsFromMaps(maps []map[string]any) []NextCall {
	if len(maps) == 0 {
		return nil
	}
	out := make([]NextCall, 0, len(maps))
	for _, entry := range maps {
		call := NextCall{
			Tool:     mapString(entry, "tool"),
			Route:    mapString(entry, "route"),
			Playbook: mapString(entry, "playbook"),
			Reason:   mapString(entry, "reason"),
		}
		if nextCallEmpty(call) {
			continue
		}
		out = append(out, call)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// appendUniqueNextCall appends a next call when no existing call shares the same
// tool, route, and playbook, keeping aggregated next calls de-duplicated and
// order-stable.
func appendUniqueNextCall(calls []NextCall, candidate NextCall) []NextCall {
	if nextCallEmpty(candidate) {
		return calls
	}
	for _, existing := range calls {
		if nextCallTargetEqual(existing, candidate) {
			return calls
		}
	}
	return append(calls, candidate)
}

// appendUniqueString appends a trimmed, non-empty string when absent, keeping
// aggregated limitations de-duplicated and order-stable.
func appendUniqueString(values []string, candidate string) []string {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return values
	}
	for _, existing := range values {
		if existing == candidate {
			return values
		}
	}
	return append(values, candidate)
}

// nextCallTargetEqual compares two next calls by their executable target (tool,
// route, playbook), ignoring the reason text so two callers recommending the
// same call with different wording collapse to one entry.
func nextCallTargetEqual(a, b NextCall) bool {
	return a.Tool == b.Tool && a.Route == b.Route && a.Playbook == b.Playbook
}

func nextCallEmpty(call NextCall) bool {
	return call.Tool == "" && call.Route == "" && call.Playbook == ""
}

func mapString(entry map[string]any, key string) string {
	if value, ok := entry[key].(string); ok {
		return value
	}
	return ""
}
