// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

type recordingDeployableUnitEdgeWriter struct {
	retractCalls []deployableUnitEdgeWriterCall
	writeCalls   []deployableUnitEdgeWriterCall
}

type deployableUnitEdgeWriterCall struct {
	domain         string
	rows           []SharedProjectionIntentRow
	evidenceSource string
}

type maintenanceReopenResolvedRelationshipLoader struct {
	generationResolved []relationships.ResolvedRelationship
	repoResolved       []relationships.ResolvedRelationship
	generationCalls    int
	repoCalls          int
	scopeID            string
	generationID       string
	repoIDs            []string
}

func (l *maintenanceReopenResolvedRelationshipLoader) GetResolvedRelationships(
	_ context.Context,
	_ string,
) ([]relationships.ResolvedRelationship, error) {
	return nil, nil
}

func (l *maintenanceReopenResolvedRelationshipLoader) GetResolvedRelationshipsForGeneration(
	_ context.Context,
	scopeID string,
	generationID string,
) ([]relationships.ResolvedRelationship, error) {
	l.generationCalls++
	l.scopeID = scopeID
	l.generationID = generationID
	return l.generationResolved, nil
}

func (l *maintenanceReopenResolvedRelationshipLoader) GetResolvedRelationshipsForRepos(
	_ context.Context,
	repoIDs []string,
) ([]relationships.ResolvedRelationship, error) {
	l.repoCalls++
	l.repoIDs = append([]string(nil), repoIDs...)
	return l.repoResolved, nil
}

func (w *recordingDeployableUnitEdgeWriter) RetractEdges(
	_ context.Context,
	domain string,
	rows []SharedProjectionIntentRow,
	evidenceSource string,
) error {
	w.retractCalls = append(w.retractCalls, deployableUnitEdgeWriterCall{
		domain:         domain,
		rows:           rows,
		evidenceSource: evidenceSource,
	})
	return nil
}

func (w *recordingDeployableUnitEdgeWriter) WriteEdges(
	_ context.Context,
	domain string,
	rows []SharedProjectionIntentRow,
	evidenceSource string,
) (SharedProjectionWriteReport, error) {
	w.writeCalls = append(w.writeCalls, deployableUnitEdgeWriterCall{
		domain:         domain,
		rows:           rows,
		evidenceSource: evidenceSource,
	})
	return SharedProjectionWriteReport{}, nil
}

func TestDeployableUnitCorrelationHandleWritesAdmittedResolvedDeploymentEdge(t *testing.T) {
	t.Parallel()

	writer := &recordingDeployableUnitEdgeWriter{}
	handler := DeployableUnitCorrelationHandler{
		FactLoader: &stubDeployableUnitFactLoader{
			envelopes: deployableUnitCorrelationEnvelopes(
				"repo-edge-api",
				"edge-api",
				[]map[string]any{
					{
						"repo_id":       "repo-edge-api",
						"language":      "dockerfile",
						"relative_path": "Dockerfile",
						"parsed_file_data": map[string]any{
							"dockerfile_stages": []any{
								map[string]any{"name": "runtime"},
							},
						},
					},
				},
			),
		},
		ResolvedLoader: &stubDeployableUnitResolvedLoader{
			resolved: []relationships.ResolvedRelationship{
				{
					SourceRepoID:     "repo-deployments",
					TargetRepoID:     "repo-edge-api",
					RelationshipType: relationships.RelDeploysFrom,
					Confidence:       0.94,
					Details: map[string]any{
						"evidence_kinds": []string{
							string(relationships.EvidenceKindArgoCDAppSource),
						},
					},
				},
			},
		},
		PhasePublisher: &recordingGraphProjectionPhasePublisher{},
		EdgeWriter:     writer,
	}

	got, err := handler.Handle(context.Background(), deployableUnitIntent("edge-api"))
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", got.CanonicalWrites)
	}
	if len(writer.retractCalls) != 1 {
		t.Fatalf("retract calls = %d, want 1", len(writer.retractCalls))
	}
	if len(writer.writeCalls) != 1 {
		t.Fatalf("write calls = %d, want 1", len(writer.writeCalls))
	}
	for _, call := range []deployableUnitEdgeWriterCall{writer.retractCalls[0], writer.writeCalls[0]} {
		if call.domain != DomainDeployableUnitEdges {
			t.Fatalf("domain = %q, want %q", call.domain, DomainDeployableUnitEdges)
		}
		if call.evidenceSource != deployableUnitCorrelationEvidenceSource {
			t.Fatalf("evidenceSource = %q, want %q", call.evidenceSource, deployableUnitCorrelationEvidenceSource)
		}
	}
	row := writer.writeCalls[0].rows[0]
	if row.RepositoryID != "repo-edge-api" {
		t.Fatalf("RepositoryID = %q, want repo-edge-api", row.RepositoryID)
	}
	for key, want := range map[string]any{
		"repo_id":             "repo-edge-api",
		"deployment_repo_id":  "repo-deployments",
		"deployable_unit_key": "edge-api",
		"correlation_key":     "repo-edge-api:edge-api",
		"admission_state":     "admitted",
		"relationship_type":   "CORRELATES_DEPLOYABLE_UNIT",
		"evidence_type":       "deployable_unit_correlation",
		"resolution_source":   "reducer/deployable-unit-correlation",
		"generation_id":       "generation-1",
		"source_system":       "git",
		"acceptance_unit_id":  "edge-api",
		"scope_id":            "repository:test-scope",
	} {
		if got := row.Payload[key]; got != want {
			t.Fatalf("payload[%s] = %#v, want %#v", key, got, want)
		}
	}
	if got := row.Payload["confidence"]; got != 0.94 {
		t.Fatalf("confidence = %#v, want 0.94", got)
	}
	if got := row.Payload["evidence_count"]; got != 9 {
		t.Fatalf("evidence_count = %#v, want 9", got)
	}
}

func TestDeployableUnitCorrelationHandleRetainsCrossScopeEdgeOnMaintenanceReopen(t *testing.T) {
	t.Parallel()

	loader := &maintenanceReopenResolvedRelationshipLoader{
		// The reopened repository intent's own relationship generation has no
		// cross-scope DEPLOYS_FROM row. The active row belongs to the deployment
		// repository's generation and is discoverable through the repo-scoped
		// read used by workload materialization.
		repoResolved: []relationships.ResolvedRelationship{
			{
				SourceRepoID:     "repo-deployments",
				TargetRepoID:     "repo-edge-api",
				RelationshipType: relationships.RelDeploysFrom,
				Confidence:       0.94,
				Details: map[string]any{
					"evidence_kinds": []string{
						string(relationships.EvidenceKindArgoCDAppSource),
					},
				},
			},
		},
	}
	writer := &recordingDeployableUnitEdgeWriter{}
	handler := DeployableUnitCorrelationHandler{
		FactLoader: &stubDeployableUnitFactLoader{
			envelopes: deployableUnitCorrelationEnvelopes(
				"repo-edge-api",
				"edge-api",
				[]map[string]any{
					{
						"repo_id":       "repo-edge-api",
						"language":      "dockerfile",
						"relative_path": "Dockerfile",
						"parsed_file_data": map[string]any{
							"dockerfile_stages": []any{
								map[string]any{"name": "runtime"},
							},
						},
					},
				},
			),
		},
		ResolvedLoader: loader,
		PhasePublisher: &recordingGraphProjectionPhasePublisher{},
		EdgeWriter:     writer,
	}

	got, err := handler.Handle(context.Background(), deployableUnitIntent("edge-api"))
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", got.CanonicalWrites)
	}
	if loader.generationCalls != 1 || loader.repoCalls != 1 {
		t.Fatalf(
			"resolved loader calls = generation:%d repo:%d, want generation:1 repo:1",
			loader.generationCalls,
			loader.repoCalls,
		)
	}
	if loader.scopeID != "repository:test-scope" || loader.generationID != "generation-1" {
		t.Fatalf(
			"generation read = (%q, %q), want (%q, %q)",
			loader.scopeID,
			loader.generationID,
			"repository:test-scope",
			"generation-1",
		)
	}
	if len(loader.repoIDs) != 1 || loader.repoIDs[0] != "repo-edge-api" {
		t.Fatalf("repo-scoped read = %v, want [repo-edge-api]", loader.repoIDs)
	}
	if len(writer.writeCalls) != 1 || len(writer.writeCalls[0].rows) != 1 {
		t.Fatalf("write calls = %#v, want one row", writer.writeCalls)
	}
	row := writer.writeCalls[0].rows[0]
	if got := row.Payload["deployment_repo_id"]; got != "repo-deployments" {
		t.Fatalf("deployment_repo_id = %#v, want repo-deployments", got)
	}
}

func TestDeployableUnitCorrelationHandleRetractsWithoutWritingDroppedCandidate(t *testing.T) {
	t.Parallel()

	writer := &recordingDeployableUnitEdgeWriter{}
	handler := DeployableUnitCorrelationHandler{
		FactLoader: &stubDeployableUnitFactLoader{
			envelopes: deployableUnitCorrelationEnvelopes(
				"repo-edge-api",
				"edge-api",
				[]map[string]any{
					{
						"repo_id":       "repo-edge-api",
						"language":      "dockerfile",
						"relative_path": "Dockerfile",
						"parsed_file_data": map[string]any{
							"dockerfile_stages": []any{
								map[string]any{"name": "runtime"},
							},
						},
					},
				},
			),
		},
		PhasePublisher: &recordingGraphProjectionPhasePublisher{},
		EdgeWriter:     writer,
	}

	got, err := handler.Handle(context.Background(), deployableUnitIntent("edge-api"))
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0", got.CanonicalWrites)
	}
	if len(writer.retractCalls) != 1 {
		t.Fatalf("retract calls = %d, want 1", len(writer.retractCalls))
	}
	if len(writer.writeCalls) != 0 {
		t.Fatalf("write calls = %d, want 0", len(writer.writeCalls))
	}
}
