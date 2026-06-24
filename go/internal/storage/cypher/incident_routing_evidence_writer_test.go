// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

func incidentRoutingEvidenceRows() []map[string]any {
	return []map[string]any{{
		"uid":                  "routing-evidence-1",
		"incident_uid":         "incident-evidence-1",
		"slot":                 "intended_routing",
		"source_class":         "declared",
		"truth_label":          "exact",
		"provider":             "pagerduty",
		"provider_incident_id": "PINCIDENT1",
		"service_id":           "PSERVICE1",
		"service_name_hash":    "hash1",
		"evidence_kind":        "content_entity.PagerDutyDeclaration",
		"evidence_id":          "declared-entity-1",
	}}
}

func TestIncidentRoutingEvidenceWriterUsesEvidenceNodesAndStaticEdges(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewIncidentRoutingEvidenceWriter(executor, 0)

	if err := writer.WriteIncidentRoutingEvidence(
		context.Background(),
		incidentRoutingEvidenceRows(),
		"scope-1",
		"gen-1",
		"reducer/incident-routing",
	); err != nil {
		t.Fatalf("WriteIncidentRoutingEvidence returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	for _, want := range []string{
		"UNWIND $rows AS row",
		"MERGE (incident:IncidentRoutingEvidence {uid: row.incident_uid})",
		"MERGE (routing:IncidentRoutingEvidence {uid: row.uid})",
		"MERGE (incident)-[rel:HAS_INTENDED_ROUTING]->(routing)",
		"routing.source_class = row.source_class",
		"routing.truth_label = row.truth_label",
		"rel.scope_id = row.scope_id",
		"rel.generation_id = row.generation_id",
		"rel.evidence_source = row.evidence_source",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("cypher missing %q:\n%s", want, cypher)
		}
	}
	for _, forbidden := range []string{
		"CloudResource",
		"KubernetesWorkload",
		"ContainerImage",
		"WorkItem",
		"PullRequest",
		"Commit",
	} {
		if strings.Contains(cypher, forbidden) {
			t.Fatalf("incident routing writer must not fabricate downstream %s nodes:\n%s", forbidden, cypher)
		}
	}
}

func TestIncidentRoutingEvidenceWriterRejectsOutOfVocabularySlot(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewIncidentRoutingEvidenceWriter(executor, 0)
	rows := incidentRoutingEvidenceRows()
	rows[0]["slot"] = "commit"

	err := writer.WriteIncidentRoutingEvidence(context.Background(), rows, "scope-1", "gen-1", "reducer/incident-routing")
	if err == nil {
		t.Fatal("WriteIncidentRoutingEvidence returned nil, want invalid slot error")
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for invalid slot", len(executor.calls))
	}
}

func TestIncidentRoutingEvidenceWriterRejectsUnsafeSlot(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewIncidentRoutingEvidenceWriter(executor, 0)
	rows := incidentRoutingEvidenceRows()
	rows[0]["slot"] = "live_routing`) DELETE n //"

	err := writer.WriteIncidentRoutingEvidence(context.Background(), rows, "scope-1", "gen-1", "reducer/incident-routing")
	if err == nil {
		t.Fatal("WriteIncidentRoutingEvidence returned nil, want unsafe slot error")
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for unsafe slot", len(executor.calls))
	}
}

func TestIncidentRoutingEvidenceWriterRetractsReducerOwnedEvidence(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewIncidentRoutingEvidenceWriter(executor, 0)

	if err := writer.RetractIncidentRoutingEvidence(
		context.Background(),
		[]string{"scope-1"},
		"gen-2",
		"reducer/incident-routing",
	); err != nil {
		t.Fatalf("RetractIncidentRoutingEvidence returned error: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(executor.calls))
	}
	cypher := executor.calls[0].Cypher
	for _, want := range []string{
		"MATCH (n:IncidentRoutingEvidence)",
		"n.scope_id IN $scope_ids",
		"n.evidence_source = $evidence_source",
		"DETACH DELETE n",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("retract cypher missing %q:\n%s", want, cypher)
		}
	}
}

func TestIncidentRoutingEvidenceWriterUsesGroupExecutorAtomically(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewIncidentRoutingEvidenceWriter(executor, 1)
	rows := append(incidentRoutingEvidenceRows(), map[string]any{
		"uid":                  "routing-evidence-2",
		"incident_uid":         "incident-evidence-1",
		"slot":                 "live_routing",
		"source_class":         "observed",
		"truth_label":          "exact",
		"provider":             "pagerduty",
		"provider_incident_id": "PINCIDENT1",
		"service_id":           "PSERVICE1",
		"evidence_kind":        "incident_routing.observed_pagerduty_service",
		"evidence_id":          "observed-fact-1",
	})

	if err := writer.WriteIncidentRoutingEvidence(context.Background(), rows, "scope-1", "gen-1", "reducer/incident-routing"); err != nil {
		t.Fatalf("WriteIncidentRoutingEvidence returned error: %v", err)
	}
	if len(executor.groupCalls) != 1 {
		t.Fatalf("len(groupCalls) = %d, want 1 atomic group", len(executor.groupCalls))
	}
	if len(executor.groupCalls[0]) != 2 {
		t.Fatalf("group statements = %d, want 2 batches", len(executor.groupCalls[0]))
	}
}
