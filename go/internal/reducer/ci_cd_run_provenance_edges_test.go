// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"testing"
)

// TestCICDRunBuiltFromRowsProjectsExactOutcomesOnly is the #5428 exact-only
// tiering guard. docs/internal/design/5472-graph-projection-policy.md:72-74
// makes exact-only promotion the rule for this edge: every non-exact outcome
// (derived/ambiguous/unresolved/rejected) stays provenance-only Postgres, so a
// row must never be built for one. A digest or repository id that is blank
// cannot anchor an endpoint, so those produce no row either rather than
// fabricating a node (#5463 "never invent an anchor").
func TestCICDRunBuiltFromRowsProjectsExactOutcomesOnly(t *testing.T) {
	t.Parallel()

	const (
		digest       = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		repositoryID = "repository:r_5428"
	)

	for _, tc := range []struct {
		name     string
		decision CICDRunCorrelationDecision
		wantRows int
	}{
		{
			name: "exact outcome with both endpoints projects one row",
			decision: CICDRunCorrelationDecision{
				Outcome:        CICDRunCorrelationExact,
				ArtifactDigest: digest,
				RepositoryID:   repositoryID,
			},
			wantRows: 1,
		},
		{
			name: "derived outcome stays postgres-only",
			decision: CICDRunCorrelationDecision{
				Outcome:        CICDRunCorrelationDerived,
				ArtifactDigest: digest,
				RepositoryID:   repositoryID,
			},
		},
		{
			name: "ambiguous outcome stays postgres-only",
			decision: CICDRunCorrelationDecision{
				Outcome:        CICDRunCorrelationAmbiguous,
				ArtifactDigest: digest,
				RepositoryID:   repositoryID,
			},
		},
		{
			name: "exact outcome without a digest anchors nothing",
			decision: CICDRunCorrelationDecision{
				Outcome:        CICDRunCorrelationExact,
				ArtifactDigest: "   ",
				RepositoryID:   repositoryID,
			},
		},
		{
			name: "exact outcome without a repository anchors nothing",
			decision: CICDRunCorrelationDecision{
				Outcome:        CICDRunCorrelationExact,
				ArtifactDigest: digest,
				RepositoryID:   "",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rows := cicdRunBuiltFromRows([]CICDRunCorrelationDecision{tc.decision})
			if len(rows) != tc.wantRows {
				t.Fatalf("cicdRunBuiltFromRows() = %d rows, want %d: %#v", len(rows), tc.wantRows, rows)
			}
			if tc.wantRows == 0 {
				return
			}
			if got := rows[0]["digest"]; got != digest {
				t.Fatalf("row digest = %v, want %q", got, digest)
			}
			if got := rows[0]["repository_id"]; got != repositoryID {
				t.Fatalf("row repository_id = %v, want %q", got, repositoryID)
			}
		})
	}
}

// TestCICDRunBuiltFromRowsDeduplicatesRepeatedDecisions proves the row set is
// idempotent under the duplicate decisions a re-projected generation can
// produce: BUILT_FROM is MERGE-based, but emitting the same (digest,
// repository) row repeatedly inflates the write budget the #5472 cost section
// caps, so the builder collapses them.
func TestCICDRunBuiltFromRowsDeduplicatesRepeatedDecisions(t *testing.T) {
	t.Parallel()

	decision := CICDRunCorrelationDecision{
		Outcome:        CICDRunCorrelationExact,
		ArtifactDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		RepositoryID:   "repository:r_dupe",
	}

	rows := cicdRunBuiltFromRows([]CICDRunCorrelationDecision{decision, decision, decision})
	if len(rows) != 1 {
		t.Fatalf("cicdRunBuiltFromRows() = %d rows, want 1 deduplicated row: %#v", len(rows), rows)
	}
}

// fakeCICDRunProvenanceEdgeWriter records the retract/write calls the
// projection makes so the orchestration order can be asserted.
type fakeCICDRunProvenanceEdgeWriter struct {
	retractCalls  int
	writeCalls    int
	retractedResp error
	lastRows      []map[string]any
	lastSource    string
	orderedCalls  []string
}

func (f *fakeCICDRunProvenanceEdgeWriter) WriteBuiltFromEdges(
	_ context.Context, rows []map[string]any, _, _, evidenceSource string,
) error {
	f.writeCalls++
	f.lastRows = rows
	f.lastSource = evidenceSource
	f.orderedCalls = append(f.orderedCalls, "write")
	return nil
}

func (f *fakeCICDRunProvenanceEdgeWriter) RetractBuiltFromEdges(
	_ context.Context, _, _, evidenceSource string,
) error {
	f.retractCalls++
	f.lastSource = evidenceSource
	f.orderedCalls = append(f.orderedCalls, "retract")
	return f.retractedResp
}

// TestProjectCICDRunBuiltFromEdgesRetractsBeforeWriting proves the
// retract-first-per-generation contract (#5472): retract runs unconditionally,
// ahead of any row check, so a generation that no longer produces an exact
// decision still removes the edge a previous generation projected. A guard that
// skipped retract when there were no rows would leave that stale edge asserting
// a build provenance the correlation no longer supports.
func TestProjectCICDRunBuiltFromEdgesRetractsBeforeWriting(t *testing.T) {
	t.Parallel()

	t.Run("no exact decisions still retracts", func(t *testing.T) {
		t.Parallel()
		writer := &fakeCICDRunProvenanceEdgeWriter{}
		handler := CICDRunCorrelationHandler{ProvenanceEdgeWriter: writer}
		if err := handler.projectCICDRunBuiltFromEdges(context.Background(), Intent{
			ScopeID: "scope-1", GenerationID: "generation-1",
		}, []CICDRunCorrelationDecision{{Outcome: CICDRunCorrelationDerived}}); err != nil {
			t.Fatalf("projectCICDRunBuiltFromEdges() error = %v, want nil", err)
		}
		if writer.retractCalls != 1 {
			t.Fatalf("retractCalls = %d, want 1 (retract must run even with zero rows)", writer.retractCalls)
		}
		if writer.writeCalls != 0 {
			t.Fatalf("writeCalls = %d, want 0 for a non-exact decision", writer.writeCalls)
		}
	})

	t.Run("exact decision retracts then writes with this domain's evidence source", func(t *testing.T) {
		t.Parallel()
		writer := &fakeCICDRunProvenanceEdgeWriter{}
		handler := CICDRunCorrelationHandler{ProvenanceEdgeWriter: writer}
		if err := handler.projectCICDRunBuiltFromEdges(context.Background(), Intent{
			ScopeID: "scope-1", GenerationID: "generation-1",
		}, []CICDRunCorrelationDecision{{
			Outcome:        CICDRunCorrelationExact,
			ArtifactDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			RepositoryID:   "repository:r_order",
		}}); err != nil {
			t.Fatalf("projectCICDRunBuiltFromEdges() error = %v, want nil", err)
		}
		if got := strings.Join(writer.orderedCalls, ","); got != "retract,write" {
			t.Fatalf("call order = %q, want \"retract,write\"", got)
		}
		if writer.lastSource != cicdRunBuiltFromProvenanceEvidenceSource {
			t.Fatalf("evidenceSource = %q, want %q — the axis that isolates this domain from #5457's BUILT_FROM edges",
				writer.lastSource, cicdRunBuiltFromProvenanceEvidenceSource)
		}
	})

	t.Run("nil writer keeps the domain postgres-only", func(t *testing.T) {
		t.Parallel()
		handler := CICDRunCorrelationHandler{}
		if err := handler.projectCICDRunBuiltFromEdges(context.Background(), Intent{}, nil); err != nil {
			t.Fatalf("projectCICDRunBuiltFromEdges() with nil writer error = %v, want nil", err)
		}
	})
}
