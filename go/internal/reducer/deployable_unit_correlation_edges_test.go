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
) error {
	w.writeCalls = append(w.writeCalls, deployableUnitEdgeWriterCall{
		domain:         domain,
		rows:           rows,
		evidenceSource: evidenceSource,
	})
	return nil
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
					SourceRepoID:     "repo-edge-api",
					TargetRepoID:     "repo-deployments",
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

func TestDeployableUnitCorrelationNoCandidateRetractIsIntentScoped(t *testing.T) {
	t.Parallel()

	writer := &recordingDeployableUnitEdgeWriter{}
	envelopes := deployableUnitCorrelationEnvelopes("repo-docs", "documentation", nil)
	envelopes = append(envelopes, deployableUnitCorrelationEnvelopes("repo-other", "other", nil)...)
	handler := DeployableUnitCorrelationHandler{
		FactLoader: &stubDeployableUnitFactLoader{
			envelopes: envelopes,
		},
		PhasePublisher: &recordingGraphProjectionPhasePublisher{},
		EdgeWriter:     writer,
	}

	_, err := handler.Handle(context.Background(), deployableUnitIntent("documentation"))
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got, want := len(writer.retractCalls), 1; got != want {
		t.Fatalf("retract calls = %d, want %d", got, want)
	}
	gotRows := writer.retractCalls[0].rows
	if got, want := len(gotRows), 1; got != want {
		t.Fatalf("retract rows = %d, want %d; rows=%#v", got, want, gotRows)
	}
	if got, want := gotRows[0].RepositoryID, "repo-docs"; got != want {
		t.Fatalf("retract row RepositoryID = %q, want %q", got, want)
	}
}
