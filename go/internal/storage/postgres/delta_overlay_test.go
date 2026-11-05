// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestDeltaOverlaySnapshotReadsTargetsOnceAndNests pins the shared-leg
// contract: one call opens one read-only repeatable-read transaction, a
// nested snapshot (a second leg, or a reader called inside another
// snapshot) reuses it without a second BEGIN, the delta-target lookup runs
// once however many legs ask for it, and the snapshot refuses writes.
func TestDeltaOverlaySnapshotReadsTargetsOnceAndNests(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{{"scope-1", "delta-1", "commit-base", ""}}},
	}}
	var legs [][]deltaOverlayTarget
	err := NewFactStore(db).withDeltaOverlaySnapshot(context.Background(), func(outer *deltaOverlaySnapshot) error {
		for range 2 {
			targets, err := outer.deltaTargets(context.Background())
			if err != nil {
				return err
			}
			legs = append(legs, targets)
		}
		if _, err := outer.store.database.ExecContext(context.Background(), "DELETE FROM fact_records"); err == nil {
			t.Fatal("snapshot ExecContext error = nil, want the read-only refusal")
		}
		return outer.store.withDeltaOverlaySnapshot(context.Background(), func(inner *deltaOverlaySnapshot) error {
			if _, err := inner.store.database.ExecContext(context.Background(), "DELETE FROM fact_records"); err == nil {
				t.Fatal("nested snapshot ExecContext error = nil, want the read-only refusal")
			}
			return nil
		})
	})
	if err != nil {
		t.Fatalf("withDeltaOverlaySnapshot() error = %v", err)
	}
	if got := len(db.queries); got != 1 || db.queries[0].query != deltaOverlayTargetsQuery {
		t.Fatalf("statements = %d, want one delta-target lookup shared by both legs", got)
	}
	if len(legs) != 2 || len(legs[0]) != 1 || legs[1][0] != legs[0][0] {
		t.Fatalf("legs = %+v, want the same one target for both", legs)
	}
	if want := deltaUnionCommitsFor("commit-base", ""); legs[0][0].commits != want {
		t.Fatalf("target commits = %+v, want %+v", legs[0][0].commits, want)
	}
	if db.beginReadOnlyRepeatableReadCalls != 1 || db.transactionCommitCalls != 1 {
		t.Fatalf("BEGIN = %d, COMMIT = %d; want one snapshot transaction for the nested call",
			db.beginReadOnlyRepeatableReadCalls, db.transactionCommitCalls)
	}
}

// manifestOverlayFakeRow is one manifest dependency row in a given scope and
// generation.
func manifestOverlayFakeRow(i int, scopeID, generationID string) []any {
	row := manifestDependencyFakeRow(i)
	row[0] = fmt.Sprintf("%s-%s-dep-%04d", scopeID, generationID, i)
	row[1], row[2] = scopeID, generationID
	return row
}

// manifestOverlayRepositoryRow is a delta repository fact that touched only
// other/package.json, so a base pin under /repo/package.json survives.
func manifestOverlayRepositoryRow(scopeID, generationID string) []any {
	return []any{
		generationID + ":repository", scopeID, generationID, "repository", "repository:" + scopeID,
		"1.0.0", "git", int64(0), "unknown", "git", "repository:" + scopeID, "", "",
		time.Date(2026, time.May, 14, 11, 0, 0, 0, time.UTC), false,
		[]byte(`{"path":"/repo","delta_generation":true,"delta_relative_paths":["other/package.json"]}`),
	}
}

// TestListActivePackageManifestDependencyFactsResolvesReachedScopesInOneBatch
// pins the #10003 round trips on the manifest reader: three reached
// delta-active scopes resolve with one base lookup and one prune probe, not
// one lookup and one probe per scope (the #9991 form sent 3 + 2 here). Scope
// 3's superseded base was pruned, so it keeps its delta rows only; the other
// two add their base pins. A target the rows never reached is not resolved or
// counted.
func TestListActivePackageManifestDependencyFactsResolvesReachedScopesInOneBatch(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{manifestOverlayFakeRow(0, "scope-1", "delta-1")}}, // active page
		{rows: [][]any{ // delta-target lookup: four delta-active scopes
			{"scope-1", "delta-1", "c1", ""},
			{"scope-2", "delta-2", "c2", ""},
			{"scope-3", "delta-3", "c3", ""},
			{"scope-4", "delta-4", "c4", ""},
		}},
		{rows: [][]any{ // base page: scopes 1 to 3 reached, scope 4 not
			manifestOverlayFakeRow(1, "scope-1", "base-1"),
			manifestOverlayFakeRow(2, "scope-2", "base-2"),
			manifestOverlayFakeRow(3, "scope-3", "base-3"),
		}},
		{rows: [][]any{ // base lookup, every reached scope at once
			{"scope-1", "base-1", "active"},
			{"scope-2", "base-2", "superseded"},
			{"scope-3", "base-3", "superseded"},
		}},
		{rows: [][]any{{retentionHashID("scope", "scope-3")}}}, // prune probe
		{rows: [][]any{ // overlay inputs for scopes 1 and 2
			manifestOverlayRepositoryRow("scope-1", "delta-1"),
			manifestOverlayRepositoryRow("scope-2", "delta-2"),
		}},
	}}
	loaded, err := NewFactStore(db).WithInstruments(instruments).ListActivePackageManifestDependencyFacts(
		context.Background(), []string{"npm"}, []string{"left-pad"},
	)
	if err != nil {
		t.Fatalf("ListActivePackageManifestDependencyFacts() error = %v", err)
	}
	if got := len(db.queries); got != 6 {
		t.Fatalf("statements = %d, want 6 (active page, targets, base page, base lookup, probe, overlay inputs)", got)
	}
	if db.queries[3].query != deltaUnionBaseQuery || db.queries[4].query != deltaUnionBasePrunedQuery {
		t.Fatalf("statements 4 and 5 = [%q %q], want the batched base lookup and probe", db.queries[3].query, db.queries[4].query)
	}
	if got, want := db.queries[3].args[0], []string{"scope-1", "scope-2", "scope-3"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("base lookup scopes = %v, want only the reached scopes %v", got, want)
	}
	var ids []string
	for _, env := range loaded {
		ids = append(ids, env.FactID)
	}
	want := []string{"scope-1-delta-1-dep-0000", "scope-1-base-1-dep-0001", "scope-2-base-2-dep-0002"}
	if fmt.Sprint(ids) != fmt.Sprint(want) {
		t.Fatalf("loaded = %v, want %v (scope 3's pruned base adds nothing)", ids, want)
	}
	if got := manifestDeltaUnionCount(t, reader, deltaOverlayBaseFound); got != 2 {
		t.Fatalf("union_base_found = %d, want 2", got)
	}
	if got := manifestDeltaUnionCount(t, reader, deltaOverlayBaseMissing); got != 1 {
		t.Fatalf("union_base_missing = %d, want 1", got)
	}
}
