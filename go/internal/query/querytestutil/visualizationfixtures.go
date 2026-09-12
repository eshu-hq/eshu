// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"github.com/eshu-hq/eshu/go/internal/query/incident/model"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Visualization-packet test fixtures shared by root's staying
// visualization_packet_surface_test.go and the visualization leaf's own
// tests (#6642 Part A). Each moved here has exactly one definition; both
// sides call it by this package-local name rather than duplicating it.

// FreshTruth returns a fresh, exact, authoritative-graph truth envelope for
// visualization-packet tests that need a source truth to preserve.
func FreshTruth() *querycontract.TruthEnvelope {
	return &querycontract.TruthEnvelope{
		Level:     querycontract.TruthLevelExact,
		Basis:     querycontract.TruthBasisAuthoritativeGraph,
		Freshness: querycontract.TruthFreshness{State: querycontract.FreshnessFresh},
	}
}

// StoryResponseWithUpstream builds a service-story dossier response whose
// upstream rows reference the given source repo ids. Order of the rows is
// the caller's responsibility so tests can shuffle it.
func StoryResponseWithUpstream(sourceRepoIDs []string) map[string]any {
	upstream := make([]map[string]any, 0, len(sourceRepoIDs))
	for _, id := range sourceRepoIDs {
		upstream = append(upstream, map[string]any{
			"source":            "repo-" + id,
			"source_repo_id":    id,
			"target_repo_id":    "svc-repo",
			"relationship_type": "DEPENDS_ON",
			"confidence":        0.9,
		})
	}
	return map[string]any{
		"service_identity": map[string]any{
			"service_id":   "svc-1",
			"service_name": "payments",
			"repo_id":      "svc-repo",
		},
		"evidence_graph": map[string]any{
			"nodes": []map[string]any{
				{"id": "svc-repo", "label": "payments-repo", "kind": "repository", "category": "service"},
			},
			"edges": []map[string]any{},
		},
		"upstream_dependencies": upstream,
		"downstream_consumers":  map[string]any{},
	}
}

// CitationResponse builds an evidence-citation response with one resolved
// citation per entity id, in the given order.
func CitationResponse(entityIDs []string) querycontract.EvidenceCitationResponse {
	citations := make([]querycontract.EvidenceCitation, 0, len(entityIDs))
	for i, id := range entityIDs {
		citations = append(citations, querycontract.EvidenceCitation{
			CitationID:     "citation:" + id,
			Rank:           i + 1,
			Kind:           "entity",
			EvidenceFamily: "source",
			EntityID:       id,
			EntityName:     "name-" + id,
			Excerpt:        "secret excerpt body",
		})
	}
	return querycontract.EvidenceCitationResponse{Question: "why?", Citations: citations}
}

// IncidentResponse builds an incident-context response with one anchor
// incident and one evidence-path edge per slot, all carrying an exact truth
// label.
func IncidentResponse(slots []model.IncidentEvidenceSlot) model.IncidentContextResponse {
	path := make([]model.IncidentContextEvidenceEdge, 0, len(slots))
	for _, slot := range slots {
		path = append(path, model.IncidentContextEvidenceEdge{
			Slot:       slot,
			TruthLabel: model.IncidentTruthExact,
		})
	}
	return model.IncidentContextResponse{
		Incident: model.IncidentContextIncident{
			Provider:           "pagerduty",
			ProviderIncidentID: "INC-1",
			Title:              "payments outage",
		},
		EvidencePath: path,
	}
}
