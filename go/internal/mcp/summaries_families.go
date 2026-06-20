package mcp

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// summarizeServiceStory renders a bounded service-story dossier summary:
// service name, API surface size + truncation, dependency/consumer counts, and
// the top limitation. It reads only from the already-parsed dossier data.
func summarizeServiceStory(data map[string]any) string {
	if data == nil {
		return ""
	}
	identity := mcpMapField(data, "service_identity")
	apiSurface := mcpMapField(data, "api_surface")
	limits := mcpMapField(data, "result_limits")
	metadata := mcpAnswerMetadata(data)

	name := query.StringVal(identity, "service_name")
	if name == "" {
		name = query.StringVal(identity, "service_id")
	}
	if name == "" {
		name = "service"
	}

	parts := []string{clampField(name)}

	endpointCount := query.IntVal(apiSurface, "endpoint_count")
	apiPart := fmt.Sprintf("API surface %d", endpointCount)
	if query.BoolVal(apiSurface, "truncated") || mcpMetadataTruncated(metadata) {
		apiPart += " (truncated)"
	}
	parts = append(parts, apiPart)

	parts = append(parts, fmt.Sprintf("deps %d", query.IntVal(limits, "upstream_count")))
	parts = append(parts, fmt.Sprintf("consumers %d", query.IntVal(limits, "downstream_count")))

	summary := strings.Join(parts, "; ")

	if limitation, count := mcpFirstMetadataReason(metadata, "limitations"); limitation != "" {
		summary += "; top limitation: " + clampField(limitation)
		if count > 1 {
			summary += fmt.Sprintf(" (+%d more)", count-1)
		}
	} else if limitations := query.StringSliceVal(identity, "limitations"); len(limitations) > 0 {
		summary += "; top limitation: " + clampField(limitations[0])
		if len(limitations) > 1 {
			summary += fmt.Sprintf(" (+%d more)", len(limitations)-1)
		}
	}
	return summary
}

// summarizeServiceInvestigation renders a bounded investigation summary: finding
// count + truncation, coverage state, and the single recommended next call.
func summarizeServiceInvestigation(data map[string]any) string {
	if data == nil {
		return ""
	}
	name := query.StringVal(data, "service_name")
	if name == "" {
		name = "service"
	}
	findingCount := mcpSliceLen(data, "investigation_findings")
	coverage := mcpMapField(data, "coverage_summary")

	summary := fmt.Sprintf("%s — %d finding(s)", clampField(name), findingCount)
	if query.BoolVal(coverage, "truncated") {
		summary += " (truncated)"
	}
	if state := query.StringVal(coverage, "state"); state != "" {
		summary += "; coverage " + state
	}
	if next := mcpFirstStringInSlice(data, "recommended_next_calls", "tool"); next != "" {
		summary += "; next: " + clampField(next)
	}
	return summary
}

// summarizeIncidentContext renders a bounded incident-context summary: incident
// title, related-change count, missing/ambiguous evidence counts, and the
// truncation marker. Missing and ambiguous counts are surfaced so a partial
// incident packet never reads as a clean success.
func summarizeIncidentContext(data map[string]any) string {
	if data == nil {
		return ""
	}
	incident := mcpMapField(data, "incident")
	title := query.StringVal(incident, "title")
	if title == "" {
		title = query.StringVal(incident, "provider_incident_id")
	}
	if title == "" {
		title = "incident"
	}

	relatedChanges := mcpSliceLen(data, "related_changes")
	missing := mcpSliceLen(data, "missing_evidence")
	ambiguous := mcpSliceLen(data, "ambiguous_evidence")
	metadata := mcpAnswerMetadata(data)
	if metadataMissing := mcpMetadataRowsLen(metadata, "missing_evidence"); metadataMissing > 0 {
		missing = metadataMissing
	}

	summary := fmt.Sprintf("%s — %d related change(s)", clampField(title), relatedChanges)
	summary += fmt.Sprintf("; missing evidence %d", missing)
	summary += fmt.Sprintf("; ambiguous %d", ambiguous)
	if query.BoolVal(data, "truncated") || mcpMetadataTruncated(metadata) {
		summary += "; truncated"
	}
	return summary
}

// summarizeCitationPacket renders a bounded evidence-citation summary: resolved
// vs requested handle counts, missing count, and the truncation marker.
func summarizeCitationPacket(data map[string]any) string {
	if data == nil {
		return ""
	}
	coverage := mcpMapField(data, "coverage")
	if coverage == nil {
		return ""
	}
	resolved := query.IntVal(coverage, "resolved_count")
	missing := query.IntVal(coverage, "missing_count")
	requested := query.IntVal(coverage, "input_handle_count")

	summary := fmt.Sprintf("coverage: resolved %d/requested %d; missing %d", resolved, requested, missing)
	if query.BoolVal(coverage, "truncated") {
		summary += " (truncated)"
	}
	return summary
}

// summarizeIndexStatus renders an actionable index/readiness summary: the health
// state and the leading reason, rather than an opaque object count.
func summarizeIndexStatus(data map[string]any) string {
	if data == nil {
		return ""
	}
	state := query.StringVal(data, "status")
	if state == "" {
		return ""
	}
	summary := "index status: " + clampField(state)
	if reasons := query.StringSliceVal(data, "reasons"); len(reasons) > 0 {
		summary += " — " + clampField(reasons[0])
		if len(reasons) > 1 {
			summary += fmt.Sprintf(" (+%d more)", len(reasons)-1)
		}
	}
	return summary
}

// summarizeHostedReadiness renders the hosted readiness result and first
// blocking class so MCP clients do not need to inspect the full check list.
func summarizeHostedReadiness(data map[string]any) string {
	if data == nil {
		return ""
	}
	state := query.StringVal(data, "state")
	if state == "" {
		return ""
	}
	summary := "hosted readiness: " + clampField(state)
	if failures := query.StringSliceVal(data, "failure_classes"); len(failures) > 0 {
		summary += " — " + clampField(failures[0])
		if len(failures) > 1 {
			summary += fmt.Sprintf(" (+%d more)", len(failures)-1)
		}
	}
	return summary
}

// summarizeIngesterStatus renders an actionable ingester readiness summary: the
// ingester name, its health state, and the leading health reason.
func summarizeIngesterStatus(data map[string]any) string {
	if data == nil {
		return ""
	}
	name := query.StringVal(data, "ingester")
	if name == "" {
		return ""
	}
	health := mcpMapField(data, "health")
	state := query.StringVal(health, "state")
	if state == "" {
		state = "unknown"
	}
	summary := fmt.Sprintf("ingester %s: %s", clampField(name), clampField(state))
	if reasons := query.StringSliceVal(health, "reasons"); len(reasons) > 0 {
		summary += " — " + clampField(reasons[0])
		if len(reasons) > 1 {
			summary += fmt.Sprintf(" (+%d more)", len(reasons)-1)
		}
	}
	return summary
}

// summarizeIngesterList renders a bounded ingester-list summary: how many
// ingesters are registered.
func summarizeIngesterList(data map[string]any) string {
	if data == nil {
		return ""
	}
	if _, ok := data["ingesters"]; !ok {
		return ""
	}
	return fmt.Sprintf("%d ingester(s) registered", mcpSliceLen(data, "ingesters"))
}

// summarizeCollectorList renders a bounded collector-list summary: how many
// collectors are registered.
func summarizeCollectorList(data map[string]any) string {
	if data == nil {
		return ""
	}
	if _, ok := data["collectors"]; !ok {
		return ""
	}
	return fmt.Sprintf("%d collector(s) registered", mcpSliceLen(data, "collectors"))
}

// summarizeCodeRelationships renders a bounded relationship-story summary that
// makes code-relationship uncertainty explicit (issue #3158): how many edges are
// derived (canonical code truth) versus heuristic/ambiguous or unsupported, and
// why the result is empty or short. It reads the structured content only and
// never reinterprets confidence or truth — it surfaces what the answer already
// labels per edge so a reader sees the ambiguity instead of trusting raw counts.
func summarizeCodeRelationships(data map[string]any) string {
	if data == nil {
		return ""
	}
	relationships, ok := data["relationships"].([]any)
	if !ok {
		return ""
	}
	var derived, heuristic, unsupported int
	for _, raw := range relationships {
		row, _ := raw.(map[string]any)
		provenance := mcpMapField(row, "provenance")
		switch query.StringVal(provenance, "truth_state") {
		case "derived":
			derived++
		case "heuristic":
			heuristic++
		default:
			unsupported++
		}
	}

	parts := []string{fmt.Sprintf("%d relationship(s): %d derived, %d heuristic/ambiguous, %d unsupported",
		len(relationships), derived, heuristic, unsupported)}
	if heuristic > 0 {
		parts = append(parts, "heuristic edges are correlation evidence, not canonical code truth")
	}
	if unsupported > 0 {
		parts = append(parts, "unsupported edges carry no recorded confidence")
	}

	coverage := mcpMapField(data, "coverage")
	if explanation := query.StringVal(coverage, "evidence_explanation"); explanation != "" {
		reason := query.StringVal(coverage, "missing_edge_reason")
		if reason != "" && reason != "complete" {
			parts = append(parts, fmt.Sprintf("%s: %s", reason, explanation))
		}
	}
	return strings.Join(parts, "; ")
}
