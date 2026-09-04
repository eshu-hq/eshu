// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// rationaleDeltaRows returns a single delta-flagged rationale refresh row, the
// shape rationale.BuildRefreshIntents emits for a repository whose delta touched
// a file.
func rationaleDeltaRows() []reducer.SharedProjectionIntentRow {
	return []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "refresh-delta",
			RepositoryID: "repo-delta",
			Payload: map[string]any{
				"repo_id":          "repo-delta",
				"delta_projection": true,
				"delta_file_paths": []string{"/repo/src/handler.go"},
			},
		},
	}
}

// deltaDeletes filters for the per-label delta DELETEs specifically, so these
// tests cannot be satisfied by the whole-scope retract that a mixed batch also
// issues.
func deltaDeletes(stmts []Statement) []Statement {
	var out []Statement
	for _, s := range stmts {
		if strings.Contains(s.Cypher, "target.path IN $file_paths") && strings.Contains(s.Cypher, "DELETE rel") {
			out = append(out, s)
		}
	}
	return out
}

// TestRetractEdgesRationaleDeltaSkipsDeletesWhenProbesFindNothing is the #5998
// F1 regression. The per-label delta retract carries the same store-size term
// as the whole-repository retract and is worse per statement: seven statements
// cost about 12s together on a 190,000-relationship store against
// 0.291s / 0.270s empty, deleting zero rows in both
// (ledger:5998-delta-per-label-retract-seeded-rerun,
// ledger:5998-delta-per-label-retract-empty). Unlike the whole-scope refresh
// this path runs on every incremental sync, so leaving it unguarded is the
// larger of the two costs.
func TestRetractEdgesRationaleDeltaSkipsDeletesWhenProbesFindNothing(t *testing.T) {
	t.Parallel()

	executor := &probeGuardRecordingExecutor{probeFound: false}
	writer := NewEdgeWriter(executor, 0)

	if err := writer.RetractEdges(
		context.Background(), reducer.DomainRationaleEdges, rationaleDeltaRows(), canonicalRationaleEvidenceSource,
	); err != nil {
		t.Fatalf("RetractEdges() error = %v, want nil", err)
	}

	if got := len(executor.probeCalls); got != len(rationaleExplainsTargetLabels) {
		t.Fatalf("probe calls = %d, want %d (one per target label)", got, len(rationaleExplainsTargetLabels))
	}
	if got := len(deltaDeletes(executor.executeCalls)); got != 0 {
		t.Fatalf("delta DELETE calls = %d, want 0 when every probe finds nothing", got)
	}
}

// TestRetractEdgesRationaleDeltaRunsDeletesWhenProbesFindRows proves the guard
// does not suppress a needed retract: with rows present every label's DELETE
// still runs. Without this, a guard that always skipped would satisfy the test
// above.
func TestRetractEdgesRationaleDeltaRunsDeletesWhenProbesFindRows(t *testing.T) {
	t.Parallel()

	executor := &probeGuardRecordingExecutor{probeFound: true}
	writer := NewEdgeWriter(executor, 0)

	if err := writer.RetractEdges(
		context.Background(), reducer.DomainRationaleEdges, rationaleDeltaRows(), canonicalRationaleEvidenceSource,
	); err != nil {
		t.Fatalf("RetractEdges() error = %v, want nil", err)
	}

	if got := len(deltaDeletes(executor.executeCalls)); got != len(rationaleExplainsTargetLabels) {
		t.Fatalf("delta DELETE calls = %d, want %d (one per target label)", got, len(rationaleExplainsTargetLabels))
	}
}

// TestRetractEdgesRationaleDeltaProbeErrorStillDeletes pins the fail-safe
// direction: a probe that errors must never be read as "nothing to delete".
func TestRetractEdgesRationaleDeltaProbeErrorStillDeletes(t *testing.T) {
	t.Parallel()

	executor := &probeGuardRecordingExecutor{probeFound: false, probeErr: errors.New("backend unavailable")}
	writer := NewEdgeWriter(executor, 0)

	if err := writer.RetractEdges(
		context.Background(), reducer.DomainRationaleEdges, rationaleDeltaRows(), canonicalRationaleEvidenceSource,
	); err != nil {
		t.Fatalf("RetractEdges() error = %v, want nil", err)
	}

	if got := len(deltaDeletes(executor.executeCalls)); got != len(rationaleExplainsTargetLabels) {
		t.Fatalf("delta DELETE calls = %d, want %d; a probe error must fall back to deleting", got, len(rationaleExplainsTargetLabels))
	}
}

// TestRetractEdgesRationaleDeltaWithoutProbeSupportDeletesEverything proves the
// guard is additive: an executor with no ProbeExecutor behaves exactly as it
// did before the guard existed.
func TestRetractEdgesRationaleDeltaWithoutProbeSupportDeletesEverything(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	if err := writer.RetractEdges(
		context.Background(), reducer.DomainRationaleEdges, rationaleDeltaRows(), canonicalRationaleEvidenceSource,
	); err != nil {
		t.Fatalf("RetractEdges() error = %v, want nil", err)
	}

	if got := len(deltaDeletes(executor.calls)); got != len(rationaleExplainsTargetLabels) {
		t.Fatalf("delta DELETE calls = %d, want %d without probe support", got, len(rationaleExplainsTargetLabels))
	}
}

// TestBuildProbeRationaleEdgeStatementsByFilePathMirrorsRetract checks two
// different things, and it is worth being precise about which is load-bearing,
// because one of them is weaker than it looks.
//
// The derived comparison (probe == retract up to the terminal clause) is
// currently true BY CONSTRUCTION: both builders call
// buildRationaleDeltaStatementsByFilePath with the predicate from
// rationaleDeltaEvidencePredicate, so as long as that stays true the assertion
// cannot fail. It is not useless -- it fails the moment someone re-forks the
// two builders into separate statement text, which is exactly the regression
// that would let probe and delete drift apart -- but it does NOT prove the
// statement text is correct today.
//
// The frozen expectation below is what proves that. It hand-writes the shipped
// Cypher for the first target label in both evidence branches, so a change to
// the MATCH, the path predicate, or the evidence predicate has to be made
// deliberately here as well. Without it this test would be a gate agreeing with
// itself: two artifacts sharing a derivation root always agree.
func TestBuildProbeRationaleEdgeStatementsByFilePathMirrorsRetract(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name                  string
		evidenceSource        string
		wantEvidencePredicate string
	}{
		{
			name:                  "multi-source canonical",
			evidenceSource:        canonicalRationaleEvidenceSource,
			wantEvidencePredicate: "rel.evidence_source IN $evidence_sources",
		},
		{
			name:                  "single-source custom",
			evidenceSource:        "custom/rationale",
			wantEvidencePredicate: "rel.evidence_source = $evidence_source",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			paths := []string{"/repo/src/handler.go"}
			retracts := BuildRetractRationaleEdgeStatementsByFilePath(paths, tc.evidenceSource)
			probes := BuildProbeRationaleEdgeStatementsByFilePath(paths, tc.evidenceSource)

			// Frozen shipped text for the first target label. Written out by
			// hand, not derived, so it can actually go red on a statement
			// change.
			if len(probes) == 0 {
				t.Fatalf("probes = 0, want %d", len(rationaleExplainsTargetLabels))
			}
			wantFirstProbe := "MATCH (rationale:Rationale)-[rel:EXPLAINS]->(target:" + rationaleExplainsTargetLabels[0] + ")\n" +
				"WHERE target.path IN $file_paths\n" +
				"  AND " + tc.wantEvidencePredicate + "\n" +
				"RETURN true LIMIT 1"
			if probes[0].Cypher != wantFirstProbe {
				t.Errorf("first probe cypher =\n%q\nwant\n%q", probes[0].Cypher, wantFirstProbe)
			}

			if len(probes) != len(retracts) {
				t.Fatalf("probes = %d, retracts = %d; they must pair one-to-one", len(probes), len(retracts))
			}
			for i := range retracts {
				want := strings.TrimSuffix(retracts[i].Cypher, rationaleDeltaRetractTerminalClause) + rationaleDeltaProbeTerminalClause
				if probes[i].Cypher != want {
					t.Errorf("probe %d cypher =\n%q\nwant\n%q", i, probes[i].Cypher, want)
				}
				if probes[i].Operation != OperationCanonicalProbe {
					t.Errorf("probe %d Operation = %q, want %q", i, probes[i].Operation, OperationCanonicalProbe)
				}
				if retracts[i].Operation != OperationCanonicalRetract {
					t.Errorf("retract %d Operation = %q, want %q", i, retracts[i].Operation, OperationCanonicalRetract)
				}
				// Parameters, not just Cypher: the evidence predicate binds a
				// value, and a probe that bound a narrower source set than its
				// paired delete would emit byte-identical Cypher while asking a
				// different question -- skipping a delete that had rows.
				if !reflect.DeepEqual(retracts[i].Parameters, probes[i].Parameters) {
					t.Errorf("probe %d parameters = %#v, want %#v (identical to its paired retract)", i, probes[i].Parameters, retracts[i].Parameters)
				}
			}
		})
	}
}
