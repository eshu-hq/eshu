// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/contract"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
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
	// Two-phase write: the incident anchor commits first so the edge
	// statements' per-slot incident MERGE only ever matches. NornicDB
	// does not provide read-your-writes across statements in one
	// transaction, so grouping the anchor create with the edge MERGEs
	// fails deterministically with a UNIQUE violation on commit.
	if len(executor.calls) != 2 {
		t.Fatalf("len(calls) = %d, want 2 (incident anchor, then edge)", len(executor.calls))
	}
	incidentCypher := executor.calls[0].Cypher
	if !strings.Contains(incidentCypher, "UNWIND $incidents AS row") {
		t.Fatalf("incident phase must UNWIND distinct incidents, got:\n%s", incidentCypher)
	}
	if !strings.Contains(incidentCypher, "MERGE (incident:IncidentRoutingEvidence {uid: row.incident_uid})") {
		t.Fatalf("incident phase must MERGE the anchor, got:\n%s", incidentCypher)
	}
	if strings.Contains(incidentCypher, ")-[rel:") {
		t.Fatalf("incident phase must not MERGE edges, got:\n%s", incidentCypher)
	}
	cypher := executor.calls[1].Cypher
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
	// The incident anchor commits in its own Execute before the edge group
	// runs: both rows share incident-evidence-1, so the group would recreate
	// it without this phase on a backend without cross-statement
	// read-your-writes.
	if len(executor.executeCalls) != 1 {
		t.Fatalf("len(executeCalls) = %d, want 1 incident-anchor commit", len(executor.executeCalls))
	}
	if !strings.Contains(executor.executeCalls[0].Cypher, "UNWIND $incidents AS row") {
		t.Fatalf("anchor commit must UNWIND distinct incidents, got:\n%s", executor.executeCalls[0].Cypher)
	}
	if len(executor.groupCalls) != 1 {
		t.Fatalf("len(groupCalls) = %d, want 1 atomic group", len(executor.groupCalls))
	}
	if len(executor.groupCalls[0]) != 2 {
		t.Fatalf("group statements = %d, want 2 batches", len(executor.groupCalls[0]))
	}
}

func TestIncidentRoutingEvidenceWriterDedupesIncidentAnchors(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewIncidentRoutingEvidenceWriter(executor, 1)

	rows := append(incidentRoutingEvidenceRows(), map[string]any{
		"uid":                  "routing-evidence-2",
		"incident_uid":         "incident-evidence-2",
		"slot":                 "live_routing",
		"source_class":         "observed",
		"truth_label":          "exact",
		"provider":             "pagerduty",
		"provider_incident_id": "PINCIDENT2",
		"service_id":           "PSERVICE2",
		"evidence_kind":        "incident_routing.observed_pagerduty_service",
		"evidence_id":          "observed-fact-2",
	})

	if err := writer.WriteIncidentRoutingEvidence(context.Background(), rows, "scope-1", "gen-1", "reducer/incident-routing"); err != nil {
		t.Fatalf("WriteIncidentRoutingEvidence returned error: %v", err)
	}
	if len(executor.executeCalls) != 1 {
		t.Fatalf("len(executeCalls) = %d, want 1 incident-anchor commit", len(executor.executeCalls))
	}
	incidents, ok := executor.executeCalls[0].Parameters["incidents"].([]map[string]any)
	if !ok {
		t.Fatalf("anchor commit params carry no incidents list: %#v", executor.executeCalls[0].Parameters)
	}
	if len(incidents) != 2 {
		t.Fatalf("anchor incidents = %d, want 2 distinct", len(incidents))
	}
	if incidents[0]["incident_uid"] != "incident-evidence-1" || incidents[1]["incident_uid"] != "incident-evidence-2" {
		t.Fatalf("anchor incidents not deduped and sorted: %#v", incidents)
	}
}

func TestIncidentRoutingEvidenceWriterAnchorErrorIsRetryable(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{errAtCall: &neo4jdriver.ConnectivityError{
		Inner: errors.New("dial tcp 10.0.0.1:7687: connect: connection refused"),
	}}
	writer := NewIncidentRoutingEvidenceWriter(executor, 0)

	err := writer.WriteIncidentRoutingEvidence(
		context.Background(),
		incidentRoutingEvidenceRows(),
		"scope-1",
		"gen-1",
		"reducer/incident-routing",
	)
	if err == nil {
		t.Fatal("WriteIncidentRoutingEvidence returned nil error, want connectivity failure")
	}
	if !contract.IsRetryable(err) {
		t.Fatalf("anchor-commit error Retryable() = false, want true: %v", err)
	}
}
