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
// the #5459 set-once writer tests below.
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

// TestOCITagFirstObservedIdentityUpsertNeverCarriesTheTimestamp proves the
// #5459 #1 constraint: the existing identity MERGE/SET statement
// (canonicalOCIImageTagObservationUpsertCypher) must never gain a
// first_observed_at self-reference. Fusing the timestamp into that statement
// is the exact regression the live prove-theory test
// (TestLiveOCITagFirstObservedProveTheory) disproved on NornicDB.
func TestOCITagFirstObservedIdentityUpsertNeverCarriesTheTimestamp(t *testing.T) {
	t.Parallel()

	exec := &recordingExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil)
	mat := tagFirstObservedMat(time.Date(2026, time.June, 25, 0, 0, 0, 0, time.UTC))

	if err := writer.Write(context.Background(), mat); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	found := false
	for _, call := range exec.calls {
		if !strings.Contains(call.Cypher, "MERGE (t:ContainerImageTagObservation:OciImageTagObservation {uid: row.uid})") {
			continue
		}
		found = true
		if strings.Contains(call.Cypher, "first_observed_at") {
			t.Fatalf("identity upsert statement must not reference first_observed_at:\n%s", call.Cypher)
		}
	}
	if !found {
		t.Fatal("expected the OCI tag-observation identity upsert statement to be dispatched")
	}
}

// TestOCITagFirstObservedSetOnceIsASeparateDeferredStatement proves the
// two-statement set-once shape: a second, separate statement MATCHes the
// already-MERGE'd node and sets first_observed_at only WHERE it is still
// null, carrying the RFC3339-UTC observed_at param. This is the exact shape
// proven live in oci_tag_first_observed_prove_theory_live_test.go.
func TestOCITagFirstObservedSetOnceIsASeparateDeferredStatement(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 25, 0, 0, 0, 0, time.UTC)
	exec := &recordingExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil)
	mat := tagFirstObservedMat(observedAt)

	if err := writer.Write(context.Background(), mat); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	var setOnce *Statement
	for i := range exec.calls {
		call := &exec.calls[i]
		if strings.Contains(call.Cypher, "WHERE t.first_observed_at IS NULL") {
			setOnce = call
			break
		}
	}
	if setOnce == nil {
		t.Fatal("expected a separate set-once statement carrying WHERE t.first_observed_at IS NULL")
	}
	if !strings.Contains(setOnce.Cypher, "MATCH (t:ContainerImageTagObservation {uid: row.uid})") {
		t.Fatalf("set-once statement must MATCH the identity-committed node by uid, got:\n%s", setOnce.Cypher)
	}
	if !strings.Contains(setOnce.Cypher, "SET t.first_observed_at = row.observed_at") {
		t.Fatalf("set-once statement must SET first_observed_at from row.observed_at, got:\n%s", setOnce.Cypher)
	}
	if strings.Contains(setOnce.Cypher, "MERGE") {
		t.Fatalf("set-once statement must not MERGE (it must only MATCH the already-committed node), got:\n%s", setOnce.Cypher)
	}

	rows, ok := setOnce.Parameters["rows"].([]map[string]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("set-once rows = %#v, want exactly one row", setOnce.Parameters["rows"])
	}
	if got, want := rows[0]["uid"], "oci-tag-observation-uid-1"; got != want {
		t.Fatalf("set-once row uid = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["observed_at"], observedAt.UTC().Format(time.RFC3339); got != want {
		t.Fatalf("set-once row observed_at = %#v, want RFC3339-UTC %#v", got, want)
	}
}

// TestOCITagFirstObservedPhaseIsInTheDeferredPackageRegistryEdgeGroup proves
// the new set-once phase partitions into the SAME deferred second-transaction
// group as the package_registry edge phases, for the identical
// multi-label-visibility reason documented on isDeferredPackageRegistryEdgePhase.
// The atomic GroupExecutor path in CanonicalNodeWriter.Write only defers
// statements this predicate selects, so a phase left out of the deferred group
// would race the identity MERGE within the same transaction and reproduce the
// last-write-wins regression the live prove-theory test caught.
func TestOCITagFirstObservedPhaseIsInTheDeferredPackageRegistryEdgeGroup(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 500, nil)
	mat := tagFirstObservedMat(time.Date(2026, time.June, 25, 0, 0, 0, 0, time.UTC))

	phases := writer.buildPhases(mat)

	var sawPhase bool
	for _, phase := range phases {
		if phase.name != canonicalPhaseOCITagFirstObserved {
			continue
		}
		sawPhase = true
		if !isDeferredPackageRegistryEdgePhase(phase.name) {
			t.Fatalf("phase %q must be classified as deferred (see isDeferredPackageRegistryEdgePhase)", phase.name)
		}
	}
	if !sawPhase {
		t.Fatalf("expected a %q phase in buildPhases() output", canonicalPhaseOCITagFirstObserved)
	}

	main, deferred := partitionDeferredPackageRegistryEdgePhases(phases)
	setOnceInDeferred := false
	for _, stmt := range deferred {
		if strings.Contains(stmt.Cypher, "WHERE t.first_observed_at IS NULL") {
			setOnceInDeferred = true
		}
	}
	if !setOnceInDeferred {
		t.Fatal("set-once statement must land in the deferred group returned by partitionDeferredPackageRegistryEdgePhases")
	}
	for _, stmt := range main {
		if strings.Contains(stmt.Cypher, "WHERE t.first_observed_at IS NULL") {
			t.Fatal("set-once statement must NOT land in the main (non-deferred) group")
		}
	}
}

// TestOCITagFirstObservedRowsSkipZeroObservedAt proves a zero-value
// ObservedAt is omitted from the set-once batch entirely (leaving
// first_observed_at null) rather than writing a zero RFC3339 timestamp.
func TestOCITagFirstObservedRowsSkipZeroObservedAt(t *testing.T) {
	t.Parallel()

	mat := tagFirstObservedMat(time.Time{})
	rows := ociImageTagFirstObservedRows(mat)
	if len(rows) != 0 {
		t.Fatalf("ociImageTagFirstObservedRows() = %#v, want no rows for a zero-value ObservedAt", rows)
	}
}

// TestOCITagFirstObservedRowsIncludeNonZeroObservedAt proves a row with
// a non-zero ObservedAt is included with the RFC3339-UTC serialization.
func TestOCITagFirstObservedRowsIncludeNonZeroObservedAt(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 25, 0, 0, 0, 0, time.UTC)
	mat := tagFirstObservedMat(observedAt)
	rows := ociImageTagFirstObservedRows(mat)
	if len(rows) != 1 {
		t.Fatalf("ociImageTagFirstObservedRows() = %#v, want exactly one row", rows)
	}
	if got, want := rows[0]["observed_at"], observedAt.UTC().Format(time.RFC3339); got != want {
		t.Fatalf("row observed_at = %#v, want %#v", got, want)
	}
	if got, want := rows[0]["uid"], "oci-tag-observation-uid-1"; got != want {
		t.Fatalf("row uid = %#v, want %#v", got, want)
	}
}
