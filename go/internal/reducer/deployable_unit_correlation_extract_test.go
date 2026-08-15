// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// TestExtractDeployableUnitCorrelationRowsReproducesHandleAdmittedEdge pins
// ExtractDeployableUnitCorrelationRows -- the pure seam Ifá's
// deployable_unit_edges materialized-edge vacuity guard (#5993) calls
// directly -- to the exact same admitted row DeployableUnitCorrelationHandler.Handle
// writes for identical input in
// TestDeployableUnitCorrelationHandleWritesAdmittedResolvedDeploymentEdge. A
// production extractor seam that only a hand-copied re-implementation
// exercises is worse than no seam: this test proves the guard calls the
// SAME code path Handle does, not a parallel one that can silently drift.
func TestExtractDeployableUnitCorrelationRowsReproducesHandleAdmittedEdge(t *testing.T) {
	t.Parallel()

	intent := deployableUnitIntent("edge-api")
	envelopes := deployableUnitCorrelationEnvelopes(
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
	)
	resolved := []relationships.ResolvedRelationship{
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
	}

	rows, evaluation, err := ExtractDeployableUnitCorrelationRows(intent, envelopes, resolved, nil)
	if err != nil {
		t.Fatalf("ExtractDeployableUnitCorrelationRows() error = %v, want nil", err)
	}
	if got, want := len(evaluation.Results), 1; got != want {
		t.Fatalf("evaluation.Results = %d, want %d", got, want)
	}

	admitted := AdmittedDeployableUnitRows(rows)
	if got, want := len(admitted), 1; got != want {
		t.Fatalf("AdmittedDeployableUnitRows(rows) = %d rows, want %d", got, want)
	}

	row := admitted[0]
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

// TestExtractDeployableUnitCorrelationRowsEmptyCandidatesYieldsNoResults
// pins the empty-candidate case Handle's own "no deployable unit candidates
// found" branch relies on: zero workload signal in the facts must produce a
// zero-Results evaluation, never an error, so Handle's guard for that branch
// stays valid after the refactor.
func TestExtractDeployableUnitCorrelationRowsEmptyCandidatesYieldsNoResults(t *testing.T) {
	t.Parallel()

	intent := deployableUnitIntent("documentation")
	envelopes := deployableUnitCorrelationEnvelopes("repo-docs", "documentation", nil)

	rows, evaluation, err := ExtractDeployableUnitCorrelationRows(intent, envelopes, nil, nil)
	if err != nil {
		t.Fatalf("ExtractDeployableUnitCorrelationRows() error = %v, want nil", err)
	}
	if len(evaluation.Results) != 0 {
		t.Fatalf("evaluation.Results = %d, want 0", len(evaluation.Results))
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %d, want 0", len(rows))
	}
}

// TestExtractDeployableUnitCorrelationRowsRequiresEntityKeys mirrors
// TestDeployableUnitCorrelationHandleRequiresEntityKeys: an intent with no
// entity keys must fail the same way through the shared pure seam.
func TestExtractDeployableUnitCorrelationRowsRequiresEntityKeys(t *testing.T) {
	t.Parallel()

	intent := deployableUnitIntent()
	envelopes := deployableUnitCorrelationEnvelopes("repo-edge-api", "edge-api", nil)

	_, _, err := ExtractDeployableUnitCorrelationRows(intent, envelopes, nil, nil)
	if err == nil {
		t.Fatal("ExtractDeployableUnitCorrelationRows() error = nil, want non-nil")
	}
}

// TestExtractDeployableUnitCorrelationRowsUsesInjectedClock proves the
// wall-clock reading rows.CreatedAt gets is fully controlled by the `now`
// parameter, never a bare time.Now() call inside the pure seam. This is what
// makes the function safe for Ifá's deployable_unit_edges vacuity guard to
// call directly: go/internal/ifa/AGENTS.md forbids wall-clock time inside
// Ifá derivation, and CreatedAt is a SharedProjectionIntentRow struct field
// (not a Payload key), so no edge-identity comparison would ever have caught
// a real-clock leak here.
func TestExtractDeployableUnitCorrelationRowsUsesInjectedClock(t *testing.T) {
	t.Parallel()

	intent := deployableUnitIntent("edge-api")
	envelopes := deployableUnitCorrelationEnvelopes(
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
	)
	resolved := []relationships.ResolvedRelationship{
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
	}
	fixed := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)

	rows, _, err := ExtractDeployableUnitCorrelationRows(intent, envelopes, resolved, func() time.Time { return fixed })
	if err != nil {
		t.Fatalf("ExtractDeployableUnitCorrelationRows() error = %v, want nil", err)
	}
	if len(rows) == 0 {
		t.Fatal("rows is empty; this guard would be vacuous")
	}
	for _, row := range rows {
		if !row.CreatedAt.Equal(fixed) {
			t.Fatalf("row.CreatedAt = %v, want the injected clock's %v", row.CreatedAt, fixed)
		}
	}
}
