// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// tagFirstObservedMat builds a single-observation CanonicalMaterialization for
// the #5459 first_observed_at writer tests below.
func tagFirstObservedMat(observedAt time.Time) projector.CanonicalMaterialization {
	return projector.CanonicalMaterialization{
		ScopeID:      "scope-oci-tag-1",
		GenerationID: "gen-oci-tag-1",
		OCIImageTagObservations: []projector.OCIImageTagObservationRow{
			{
				UID:            "oci-tag-observation-uid-1",
				RepositoryID:   "oci-registry://ghcr.io/eshu-hq/demo",
				ImageRef:       "ghcr.io/eshu-hq/demo:1.0.0",
				Tag:            "1.0.0",
				ResolvedDigest: "sha256:" + strings.Repeat("a", 64),
				ObservedAt:     observedAt,
			},
		},
	}
}

// ociTagIdentityUpsertStatement returns the single dispatched identity-upsert
// statement for the tag-observation mat, failing the test if it is absent.
func ociTagIdentityUpsertStatement(t *testing.T, mat projector.CanonicalMaterialization) Statement {
	t.Helper()
	exec := &recordingExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil)
	if err := writer.Write(context.Background(), mat); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	for _, call := range exec.calls {
		if strings.Contains(call.Cypher, "MERGE (t:ContainerImageTagObservation:OciImageTagObservation {uid: row.uid})") {
			return call
		}
	}
	t.Fatal("expected the OCI tag-observation identity upsert statement to be dispatched")
	return Statement{}
}

// TestOCITagFirstObservedIsWrittenOnCreateOnly proves the #5459 set-once
// contract: first_observed_at is written by ON CREATE SET inside the single
// identity MERGE, so it is fixed at node creation and never overwritten by a
// later observation of the same uid. It must NOT appear in the unconditional
// SET clause (that would make it last-write-wins), and there is no separate
// deferred statement — ON CREATE reads no persisted property, so it sidesteps
// both the NornicDB same-statement self-reference shadow and the
// cross-transaction multi-label MATCH gap, as proven live in
// TestLiveOCITagFirstObservedProveTheory.
func TestOCITagFirstObservedIsWrittenOnCreateOnly(t *testing.T) {
	t.Parallel()

	stmt := ociTagIdentityUpsertStatement(t, tagFirstObservedMat(time.Date(2026, time.June, 25, 0, 0, 0, 0, time.UTC)))

	if !strings.Contains(stmt.Cypher, "ON CREATE SET t.first_observed_at = row.observed_at") {
		t.Fatalf("identity upsert must write first_observed_at via ON CREATE SET, got:\n%s", stmt.Cypher)
	}
	// first_observed_at must appear exactly once — only in the ON CREATE SET
	// clause, never in the unconditional SET that runs on every match.
	if got := strings.Count(stmt.Cypher, "first_observed_at"); got != 1 {
		t.Fatalf("first_observed_at must appear exactly once (ON CREATE SET only), got %d occurrences:\n%s", got, stmt.Cypher)
	}
}

// TestOCITagObservationRowCarriesObservedAt proves the identity row builder
// carries the RFC3339-UTC observed_at value the ON CREATE SET consumes.
func TestOCITagObservationRowCarriesObservedAt(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 25, 0, 0, 0, 0, time.UTC)
	rows := ociImageTagObservationRows(tagFirstObservedMat(observedAt))
	if len(rows) != 1 {
		t.Fatalf("ociImageTagObservationRows() = %#v, want exactly one row", rows)
	}
	if got, want := rows[0]["observed_at"], observedAt.UTC().Format(time.RFC3339); got != want {
		t.Fatalf("row observed_at = %#v, want RFC3339-UTC %#v", got, want)
	}
	if got, want := rows[0]["uid"], "oci-tag-observation-uid-1"; got != want {
		t.Fatalf("row uid = %#v, want %#v", got, want)
	}
}

// TestOCITagObservedAtValueZeroIsEmpty proves a zero-value ObservedAt
// serializes to "" (so ON CREATE SET stores no meaningful first_observed_at and
// the reader omits it) rather than the Unix epoch RFC3339 string.
func TestOCITagObservedAtValueZeroIsEmpty(t *testing.T) {
	t.Parallel()

	if got := ociTagObservedAtValue(time.Time{}); got != "" {
		t.Fatalf("ociTagObservedAtValue(zero) = %q, want empty string", got)
	}
	observedAt := time.Date(2026, time.June, 25, 0, 0, 0, 0, time.UTC)
	if got, want := ociTagObservedAtValue(observedAt), observedAt.UTC().Format(time.RFC3339); got != want {
		t.Fatalf("ociTagObservedAtValue(%v) = %q, want %q", observedAt, got, want)
	}
}
