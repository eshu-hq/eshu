// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package model

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

var incidentEvidenceSlotOrder = []IncidentEvidenceSlot{
	IncidentSlotIncident,
	IncidentSlotService,
	IncidentSlotIntendedRouting,
	IncidentSlotAppliedRouting,
	IncidentSlotLiveRouting,
	IncidentSlotDeployable,
	IncidentSlotRuntimeArtifact,
	IncidentSlotImage,
	IncidentSlotBuildDeploy,
	IncidentSlotCommit,
	IncidentSlotPullRequest,
	IncidentSlotWorkItem,
}

// BuildIncidentContextResponse applies the public incident-context contract to
// a store snapshot. Every expected path slot is present, even when evidence is
// currently missing.
func BuildIncidentContextResponse(snapshot IncidentContextSnapshot) IncidentContextResponse {
	edges := map[IncidentEvidenceSlot]IncidentContextEvidenceEdge{}
	for _, edge := range snapshot.EvidencePath {
		edge = normalizeIncidentContextEdge(edge)
		if edge.Slot != "" {
			edges[edge.Slot] = edge
		}
	}
	if _, ok := edges[IncidentSlotIncident]; !ok {
		edges[IncidentSlotIncident] = incidentAnchorEdge(snapshot.Incident)
	}
	if _, ok := edges[IncidentSlotService]; !ok {
		edges[IncidentSlotService] = incidentServiceEdge(snapshot.Incident)
	}
	for _, slot := range incidentEvidenceSlotOrder {
		if _, ok := edges[slot]; !ok {
			edges[slot] = MissingEdge(slot)
		}
	}

	path := orderedIncidentContextEdges(edges)
	missing := make([]IncidentMissingEvidence, 0)
	ambiguous := make([]IncidentContextEvidenceEdge, 0)
	for _, edge := range path {
		switch edge.TruthLabel {
		case IncidentTruthMissing, IncidentTruthUnresolved, IncidentTruthStale,
			IncidentTruthPermissionHidden, IncidentTruthRejected:
			missing = append(missing, IncidentMissingEvidence{
				Slot:   edge.Slot,
				Reason: edge.Explanation,
			})
		case IncidentTruthAmbiguous:
			ambiguous = append(ambiguous, edge)
		case IncidentTruthExact, IncidentTruthDerived, IncidentTruthFallback,
			IncidentTruthDrifted:
			// Present evidence: the edge stands as built, deriving neither a
			// missing nor an ambiguous entry.
		}
	}

	changes := append([]IncidentContextChangeCandidate(nil), snapshot.RelatedChanges...)
	if changes == nil {
		changes = []IncidentContextChangeCandidate{}
	}
	for idx := range changes {
		if changes[idx].TruthLabel == "" {
			changes[idx].TruthLabel = IncidentTruthFallback
		}
		if changes[idx].Explanation == "" {
			changes[idx].Explanation = "candidate matched PagerDuty service and incident time window"
		}
	}
	timeline := append([]IncidentContextTimelineEvent(nil), snapshot.Timeline...)
	if timeline == nil {
		timeline = []IncidentContextTimelineEvent{}
	}
	if missing == nil {
		missing = []IncidentMissingEvidence{}
	}
	if ambiguous == nil {
		ambiguous = []IncidentContextEvidenceEdge{}
	}

	response := IncidentContextResponse{
		Query:             snapshot.Query,
		Incident:          snapshot.Incident,
		Timeline:          timeline,
		RelatedChanges:    changes,
		EvidencePath:      path,
		MissingEvidence:   missing,
		AmbiguousEvidence: ambiguous,
		Truncated:         snapshot.Truncated,
	}
	response.AnswerMetadata = incidentContextAnswerMetadata(response)
	return response
}

func normalizeIncidentContextEdge(edge IncidentContextEvidenceEdge) IncidentContextEvidenceEdge {
	if edge.TruthLabel == "" {
		if len(edge.Candidates) > 1 {
			edge.TruthLabel = IncidentTruthAmbiguous
		} else if len(edge.Evidence) > 0 || len(edge.Value) > 0 {
			edge.TruthLabel = IncidentTruthDerived
		} else {
			edge.TruthLabel = IncidentTruthMissing
		}
	}
	if edge.Explanation == "" {
		edge.Explanation = defaultIncidentContextExplanation(edge.Slot, edge.TruthLabel)
	}
	return edge
}

func incidentAnchorEdge(incident IncidentContextIncident) IncidentContextEvidenceEdge {
	if incident.ProviderIncidentID == "" {
		return MissingEdge(IncidentSlotIncident)
	}
	return IncidentContextEvidenceEdge{
		Slot:        IncidentSlotIncident,
		TruthLabel:  IncidentTruthExact,
		Explanation: "provider incident record matched by provider_incident_id",
		Value: map[string]string{
			"provider":             incident.Provider,
			"provider_incident_id": incident.ProviderIncidentID,
		},
		Evidence: []IncidentContextEvidenceRef{
			{
				FactID:     incident.EvidenceFactID,
				Source:     incident.Provider,
				Kind:       "incident.record",
				URL:        incident.SourceURL,
				Confidence: incident.SourceConfidence,
				ObservedAt: incident.ObservedAt,
			},
		},
	}
}

func incidentServiceEdge(incident IncidentContextIncident) IncidentContextEvidenceEdge {
	if incident.Service.ID == "" && incident.Service.Summary == "" {
		return MissingEdge(IncidentSlotService)
	}
	return IncidentContextEvidenceEdge{
		Slot:        IncidentSlotService,
		TruthLabel:  IncidentTruthExact,
		Explanation: "provider incident record reported the affected service",
		Value: map[string]string{
			"service_id":   incident.Service.ID,
			"service_name": incident.Service.Summary,
			"service_url":  incident.Service.URL,
		},
		Evidence: []IncidentContextEvidenceRef{
			{
				FactID:     incident.EvidenceFactID,
				Source:     incident.Provider,
				Kind:       "incident.record",
				URL:        incident.SourceURL,
				Confidence: incident.SourceConfidence,
				ObservedAt: incident.ObservedAt,
			},
		},
	}
}

// MissingEdge returns the contract's missing-evidence edge for slot: the
// Missing truth label with the slot's documented explanation. It lives in
// incident/model/ (#6060, lane B S2) because both the response assembly here
// and the routing evidence builder in incident/store/ fall back to it, and
// the two must never disagree on what "missing" means for a slot.
func MissingEdge(slot IncidentEvidenceSlot) IncidentContextEvidenceEdge {
	return IncidentContextEvidenceEdge{
		Slot:        slot,
		TruthLabel:  IncidentTruthMissing,
		Explanation: defaultIncidentContextExplanation(slot, IncidentTruthMissing),
	}
}

func defaultIncidentContextExplanation(
	slot IncidentEvidenceSlot,
	label IncidentTruthLabel,
) string {
	if label != IncidentTruthMissing {
		return "incident context evidence was reported by the read model"
	}
	switch slot {
	case IncidentSlotIncident:
		return "no provider incident record matched the requested incident id"
	case IncidentSlotService:
		return "no affected service was reported on the incident record"
	case IncidentSlotIntendedRouting:
		return "no Terraform-declared PagerDuty routing evidence has been linked to this incident service yet"
	case IncidentSlotAppliedRouting:
		return "no applied Terraform-state PagerDuty service evidence has been linked to this incident service yet"
	case IncidentSlotLiveRouting:
		return "no live PagerDuty service configuration evidence has been linked to this incident service yet"
	case IncidentSlotDeployable:
		return "no deployable or workload mapping evidence has been linked to this incident yet"
	case IncidentSlotRuntimeArtifact:
		return "no runtime artifact evidence has been linked to this incident yet"
	case IncidentSlotImage:
		return "no image tag, digest, or version evidence has been linked to this incident yet"
	case IncidentSlotBuildDeploy:
		return "no build or deploy record evidence has been linked to this incident yet"
	case IncidentSlotCommit:
		return "no commit evidence has been linked to this incident yet"
	case IncidentSlotPullRequest:
		return "no pull request evidence has been linked to this incident yet"
	case IncidentSlotWorkItem:
		return "no Jira or external work-item evidence has been linked to this incident yet"
	default:
		return "no evidence is available for this incident path slot"
	}
}

// incidentContextAnswerMetadata derives the normalized answer companion from
// an already-built response payload. It moved here from the query root's
// answer_metadata_alias.go with the incident-context response assembly
// (#6060, lane B S2): BuildIncidentContextResponse above is its only caller.
func incidentContextAnswerMetadata(response IncidentContextResponse) querycontract.AnswerMetadata {
	missing := make([]map[string]any, 0, len(response.MissingEvidence))
	for _, item := range response.MissingEvidence {
		missing = append(missing, map[string]any{
			"slot":   string(item.Slot),
			"reason": item.Reason,
		})
	}
	evidence := make([]map[string]any, 0)
	for _, edge := range response.EvidencePath {
		for _, ref := range edge.Evidence {
			if ref.FactID == "" {
				continue
			}
			evidence = append(evidence, map[string]any{
				"kind":            "fact",
				"evidence_family": string(edge.Slot),
				"entity_id":       ref.FactID,
				"reason":          edge.Explanation,
			})
		}
	}
	data := map[string]any{
		"evidence_handles": evidence,
		"missing_evidence": missing,
		"truncated":        response.Truncated,
		"coverage": map[string]any{
			"query_shape":          "incident_context_evidence_path",
			"evidence_path_slots":  len(response.EvidencePath),
			"missing_evidence":     len(response.MissingEvidence),
			"ambiguous_evidence":   len(response.AmbiguousEvidence),
			"related_change_count": len(response.RelatedChanges),
			"truncated":            response.Truncated,
		},
	}
	return querycontract.BuildAnswerMetadata(data)
}

func orderedIncidentContextEdges(
	edges map[IncidentEvidenceSlot]IncidentContextEvidenceEdge,
) []IncidentContextEvidenceEdge {
	out := make([]IncidentContextEvidenceEdge, 0, len(edges))
	seen := map[IncidentEvidenceSlot]struct{}{}
	for _, slot := range incidentEvidenceSlotOrder {
		if edge, ok := edges[slot]; ok {
			out = append(out, edge)
			seen[slot] = struct{}{}
		}
	}
	var extra []string
	for slot := range edges {
		if _, ok := seen[slot]; !ok {
			extra = append(extra, string(slot))
		}
	}
	sort.Strings(extra)
	for _, slot := range extra {
		out = append(out, edges[IncidentEvidenceSlot(slot)])
	}
	return out
}
