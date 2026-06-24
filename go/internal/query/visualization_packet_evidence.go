// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "strings"

// BuildEvidenceCitationVisualizationPacket derives a visualization packet from
// an existing evidence-citation response. Each resolved citation becomes one
// node whose stable ID is derived from the citation handle identity
// (entity id, or repo_id+relative_path), and whose EvidenceHandle points back to
// the citation handle shape. The citation packet has no relationships, so the
// packet carries nodes with no synthetic edges. The builder reads only the
// citations the response already resolved and never re-queries content.
func BuildEvidenceCitationVisualizationPacket(response evidenceCitationResponse, truth *TruthEnvelope) VisualizationPacket {
	if len(response.Citations) == 0 {
		return unsupportedVisualizationPacket(
			VisualizationViewEvidenceCitation,
			truth,
			[]string{"evidence citation response resolved no citations to visualize"},
			evidenceCitationVisualizationNextCalls(response),
		)
	}

	builder := newVisualizationBuilder(VisualizationViewEvidenceCitation, strings.TrimSpace(response.Question))
	builder.truth = truth
	for _, citation := range response.Citations {
		anchor, family := citationVisualizationIdentity(citation)
		if anchor == "" {
			continue
		}
		builder.addNode(VisualizationNode{
			ID:             visualizationNodeID("citation", anchor),
			Type:           "citation",
			Label:          citationVisualizationLabel(citation),
			Category:       firstNonEmptyString(citation.EvidenceFamily, family),
			EvidenceHandle: citationVisualizationHandle(citation),
		})
	}

	packet := builder.finalize()
	if response.Coverage.Truncated {
		packet.Truncation.Truncated = true
		packet.Limitations = appendReason(packet.Limitations,
			"source citation response was already truncated; visualized nodes are a bounded subset")
	}
	if len(response.MissingHandles) > 0 {
		packet.Limitations = appendReason(packet.Limitations,
			"some requested evidence handles were unresolved and are not shown as nodes")
	}
	return packet
}

// citationVisualizationIdentity returns the stable identity anchor and a default
// evidence family for one citation. The anchor prefers the entity id, then
// repo_id+relative_path, so equal citations always yield the same node ID.
func citationVisualizationIdentity(citation evidenceCitation) (string, string) {
	if entity := strings.TrimSpace(citation.EntityID); entity != "" {
		return "entity\x00" + entity, "source"
	}
	repoID := strings.TrimSpace(citation.RepoID)
	relPath := strings.TrimSpace(citation.RelativePath)
	if repoID != "" && relPath != "" {
		return "file\x00" + repoID + "\x00" + relPath, "file"
	}
	return "", ""
}

func citationVisualizationLabel(citation evidenceCitation) string {
	return firstNonEmptyString(citation.EntityName, citation.RelativePath, citation.EntityID, citation.CitationID)
}

// citationVisualizationHandle rebuilds the evidence_citation handle for a
// citation node from fields the citation already carried, so a node maps back to
// a citation handle without inventing new fields.
func citationVisualizationHandle(citation evidenceCitation) *evidenceCitationHandle {
	handle := &evidenceCitationHandle{
		Kind:           citation.Kind,
		RepoID:         citation.RepoID,
		RelativePath:   citation.RelativePath,
		EntityID:       citation.EntityID,
		EvidenceFamily: citation.EvidenceFamily,
		Reason:         citation.Reason,
		StartLine:      citation.StartLine,
		EndLine:        citation.EndLine,
	}
	return handle
}

func evidenceCitationVisualizationNextCalls(response evidenceCitationResponse) []map[string]any {
	if len(response.RecommendedNextCalls) > 0 {
		return response.RecommendedNextCalls
	}
	return []map[string]any{
		{
			"tool":   "build_evidence_citation_packet",
			"reason": "resolve evidence handles into citations before visualizing the evidence graph",
		},
	}
}

// BuildIncidentContextVisualizationPacket derives a visualization packet from an
// existing incident-context response. The incident anchor is one node; each
// evidence-path edge contributes a slot node, and consecutive slots are joined
// by an edge that carries the slot's truth label straight from the response. The
// builder reads only the evidence path the response already returned.
func BuildIncidentContextVisualizationPacket(response IncidentContextResponse, truth *TruthEnvelope) VisualizationPacket {
	if len(response.EvidencePath) == 0 {
		return unsupportedVisualizationPacket(
			VisualizationViewIncidentContext,
			truth,
			[]string{"incident context response carried no evidence path to visualize"},
			incidentVisualizationNextCalls(),
		)
	}

	builder := newVisualizationBuilder(VisualizationViewIncidentContext, incidentVisualizationTitle(response.Incident))
	builder.truth = truth

	incidentNodeID := addIncidentAnchorNode(builder, response.Incident)
	prevNodeID := incidentNodeID
	for _, edge := range response.EvidencePath {
		slot := strings.TrimSpace(string(edge.Slot))
		if slot == "" {
			continue
		}
		slotNodeID := visualizationNodeID("incident_slot", incidentVisualizationAnchor(response.Incident), slot)
		builder.addNode(VisualizationNode{
			ID:         slotNodeID,
			Type:       "evidence_slot",
			Label:      slot,
			Category:   slot,
			TruthLabel: string(edge.TruthLabel),
		})
		if prevNodeID != "" {
			builder.addEdge(VisualizationEdge{
				Source:       prevNodeID,
				Target:       slotNodeID,
				Relationship: "EVIDENCE_PATH",
				TruthLabel:   string(edge.TruthLabel),
			})
		}
		prevNodeID = slotNodeID
	}

	packet := builder.finalize()
	if response.Truncated {
		packet.Truncation.Truncated = true
		packet.Limitations = appendReason(packet.Limitations,
			"source incident response was already truncated; visualized path is a bounded subset")
	}
	if len(response.MissingEvidence) > 0 {
		packet.Limitations = appendReason(packet.Limitations,
			"some incident evidence slots were missing and are not shown as nodes")
	}
	return packet
}

// addIncidentAnchorNode adds the incident anchor node and returns its stable ID.
// The ID is derived from the provider plus provider incident id, so the same
// incident always yields the same anchor.
func addIncidentAnchorNode(builder *visualizationBuilder, incident IncidentContextIncident) string {
	anchor := incidentVisualizationAnchor(incident)
	if anchor == "" {
		return ""
	}
	nodeID := visualizationNodeID("incident", anchor)
	builder.addNode(VisualizationNode{
		ID:       nodeID,
		Type:     "incident",
		Label:    incidentVisualizationTitle(incident),
		Category: "incident",
	})
	return nodeID
}

func incidentVisualizationAnchor(incident IncidentContextIncident) string {
	provider := strings.TrimSpace(incident.Provider)
	id := strings.TrimSpace(incident.ProviderIncidentID)
	if provider == "" && id == "" {
		return ""
	}
	return provider + "\x00" + id
}

func incidentVisualizationTitle(incident IncidentContextIncident) string {
	if title := strings.TrimSpace(incident.Title); title != "" {
		return title
	}
	return strings.TrimSpace(incident.ProviderIncidentID)
}

func incidentVisualizationNextCalls() []map[string]any {
	return []map[string]any{
		{
			"tool":   "get_incident_context",
			"reason": "fetch an incident context response with an evidence path before visualizing",
		},
	}
}
