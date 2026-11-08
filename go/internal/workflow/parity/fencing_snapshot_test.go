// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parity

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/claim/fence"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// rehearsalRows serves a fixed result set for the snapshot and teardown
// doubles. It supports the *string, *int64, and *int scan targets the
// harness uses.
type rehearsalRows struct {
	values [][]any
	index  int
}

func (r *rehearsalRows) Next() bool { r.index++; return r.index <= len(r.values) }
func (r *rehearsalRows) Err() error { return nil }
func (r *rehearsalRows) Close() error {
	return nil
}

func (r *rehearsalRows) Scan(dest ...any) error {
	row := r.values[r.index-1]
	for i := range dest {
		switch target := dest[i].(type) {
		case *string:
			*target = row[i].(string)
		case *int64:
			*target = row[i].(int64)
		case *int:
			*target = row[i].(int)
		default:
			return errors.New("unsupported scan target")
		}
	}
	return nil
}

// failingScanRows fails the first Scan so the snapshot error path is covered.
type failingScanRows struct{ rehearsalRows }

func (r *failingScanRows) Scan(...any) error { return errors.New("scan boom") }

type failingScanDB struct{}

func (failingScanDB) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return &failingScanRows{rehearsalRows: rehearsalRows{values: [][]any{{"x"}}}}, nil
}

// rehearsalQueryDB answers one result set and records the query text.
type rehearsalQueryDB struct {
	rows    [][]any
	queries []string
	args    [][]any
}

func (d *rehearsalQueryDB) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	d.queries = append(d.queries, query)
	d.args = append(d.args, args)
	return &rehearsalRows{values: d.rows}, nil
}

// rehearsalResult is a scripted sql.Result.
type rehearsalResult struct{ affected int64 }

func (r rehearsalResult) LastInsertId() (int64, error) { return 0, nil }
func (r rehearsalResult) RowsAffected() (int64, error) { return r.affected, nil }

// rehearsalExecDB records every statement and answers the residue count.
type rehearsalExecDB struct {
	affected []int64
	residue  int64
	stmts    []string
	args     [][]any
}

func (d *rehearsalExecDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	d.stmts = append(d.stmts, query)
	d.args = append(d.args, args)
	n := int64(0)
	if len(d.affected) > 0 {
		n, d.affected = d.affected[0], d.affected[1:]
	}
	return rehearsalResult{affected: n}, nil
}

func (d *rehearsalExecDB) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	d.stmts = append(d.stmts, query)
	d.args = append(d.args, args)
	return &rehearsalRows{values: [][]any{{d.residue}}}, nil
}

// rehearsalFenceDB answers the fence upsert and the classification read.
type rehearsalFenceDB struct {
	upsertRows [][]any
	readRows   [][]any
}

func (d *rehearsalFenceDB) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	if query == fence.UpsertQuery {
		return &rehearsalRows{values: d.upsertRows}, nil
	}
	return &rehearsalRows{values: d.readRows}, nil
}

func (d *rehearsalFenceDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("fence never execs")
}

// flakyPinner scripts pin answers and records identical-argument retries.
type flakyPinner struct {
	errs     []error
	commitID string
	calls    int
	argSets  [][3]string
}

func (p *flakyPinner) PinClaimForCommit(_ context.Context, claimID string, _ int64,
	scopeID, sourceRunID, generationID string, _ time.Time,
) (string, error) {
	p.calls++
	p.argSets = append(p.argSets, [3]string{claimID, scopeID, generationID})
	if len(p.errs) > 0 {
		err := p.errs[0]
		p.errs = p.errs[1:]
		if err != nil {
			return "", err
		}
	}
	return p.commitID, nil
}

// TestFencingRehearsalStaleCommitRace is the #9376 criterion-3 race: the
// fresh writer's fence write lands, the stale writer's is refused terminally,
// and the harness classifies exactly that pair as a proven race.
func TestFencingRehearsalStaleCommitRace(t *testing.T) {
	if RehearsalInstanceID != "synth-8875-rehearsal-leg-1" {
		t.Fatalf("RehearsalInstanceID = %q, want the satellite-declared seed instance", RehearsalInstanceID)
	}
	fresh := fence.Row{
		GenerationID: "gen-r1", WorkItemID: "parity-fence-item-1",
		ScopeID: "scope-1", ClaimID: "claim-fresh", FencingToken: 2, CommitID: "commit-fresh-2",
	}
	stale := fresh
	stale.ClaimID = "claim-stale"
	stale.FencingToken = 1
	stale.CommitID = "commit-stale-1"

	freshDB := &rehearsalFenceDB{upsertRows: [][]any{{int64(2)}}}
	if err := fence.Write(context.Background(), freshDB, fresh); err != nil {
		t.Fatalf("fresh Write() error = %v, want accept", err)
	}
	staleDB := &rehearsalFenceDB{readRows: [][]any{{"claim-fresh", int64(2)}}}
	staleErr := fence.Write(context.Background(), staleDB, stale)
	var rejection *fence.Rejection
	if !errors.As(staleErr, &rejection) || rejection.Reason != fence.ReasonFenceToken {
		t.Fatalf("stale Write() error = %v, want terminal fence_token", staleErr)
	}
	if err := CheckStaleRace(nil, staleErr); err != nil {
		t.Fatalf("CheckStaleRace(fresh-ok, stale-refused) = %v, want nil", err)
	}
	if err := CheckStaleRace(nil, nil); err == nil {
		t.Fatalf("CheckStaleRace(nil, nil) = nil, want an error: an accepted stale writer is not a proven race")
	}
	if err := CheckStaleRace(errors.New("fresh commit failed"), staleErr); err == nil {
		t.Fatalf("CheckStaleRace(fresh-err, _) = nil, want an error")
	}
	retryable := &fence.Rejection{Reason: fence.ReasonPinUnavailable}
	if err := CheckStaleRace(nil, retryable); err == nil {
		t.Fatalf("CheckStaleRace(nil, retryable) = nil, want an error: only terminal refusals prove fencing")
	}
}

// TestFencingRehearsalAmbiguousTimeoutReplay proves the retry after an
// ambiguous pin timeout reuses identical arguments and keeps the plane's
// first commit id.
func TestFencingRehearsalAmbiguousTimeoutReplay(t *testing.T) {
	ctx := context.Background()
	req := fence.Request{
		WorkItemID: "parity-fence-item-1", ClaimID: "claim-1",
		FencingToken: 1, ScopeID: "scope-1", SourceRunID: "sr-1", GenerationID: "gen-1",
		Lease: time.Minute,
	}
	pinner := &flakyPinner{errs: []error{errors.New("timeout: no response")}, commitID: "commit-first"}
	got, err := ReplayAmbiguousPin(ctx, pinner, func(error) string { return "" }, req, time.Now().Add(time.Minute))
	if err != nil || got != "commit-first" {
		t.Fatalf("ReplayAmbiguousPin() = %q, %v; want %q, nil", got, err, "commit-first")
	}
	if pinner.calls != 2 {
		t.Fatalf("pin calls = %d, want 2 (timeout then retry)", pinner.calls)
	}
	if pinner.argSets[0] != pinner.argSets[1] {
		t.Fatalf("retry args differ: %v vs %v; an ambiguous retry must repeat identical arguments", pinner.argSets[0], pinner.argSets[1])
	}
	terminal := &flakyPinner{errs: []error{errors.New("stale")}, commitID: "commit-never"}
	if _, err := ReplayAmbiguousPin(ctx, terminal, func(error) string { return "stale_fence" }, req, time.Now()); err == nil {
		t.Fatalf("ReplayAmbiguousPin(stale) = nil error, want the terminal refusal")
	}
	if terminal.calls != 1 {
		t.Fatalf("terminal pin calls = %d, want 1 (no retry on a terminal refusal)", terminal.calls)
	}
}

// TestFencingRehearsalHighWaterSnapshot proves the snapshot read, the
// digest-as-field record, and the high-water assertion over one prefix.
func TestFencingRehearsalHighWaterSnapshot(t *testing.T) {
	database := &rehearsalQueryDB{rows: [][]any{
		{"gen-r1", "parity-fence-item-1", "claim-fresh", int64(2)},
		{"gen-r1", "parity-fence-item-2", "claim-other", int64(1)},
	}}
	rows, err := ReadFencingSnapshot(context.Background(), database, "parity-fence-%")
	if err != nil {
		t.Fatalf("ReadFencingSnapshot() error = %v", err)
	}
	if len(rows) != 2 || rows[0].FencingToken != 2 || rows[1].ClaimID != "claim-other" {
		t.Fatalf("snapshot rows = %+v, want the two committed maxima in order", rows)
	}
	query := database.queries[0]
	for _, fragment := range []string{"generation_commit_fences", "ORDER BY", "LIKE"} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("snapshot query missing %q:\n%s", fragment, query)
		}
	}
	first, second := SnapshotDigest(rows), SnapshotDigest(rows)
	if first == "" || first != second {
		t.Fatalf("SnapshotDigest unstable: %q vs %q", first, second)
	}
	if len(first) != 64 {
		t.Fatalf("SnapshotDigest = %q, want 64 hex characters of sha256", first)
	}
	moved := append([]FencingSnapshotRow(nil), rows...)
	moved[0].FencingToken = 3
	if SnapshotDigest(moved) == first {
		t.Fatalf("SnapshotDigest ignores the fencing token: moved high-water keeps digest %q", first)
	}
	want := map[FencingHighWaterKey]int64{
		{GenerationID: "gen-r1", WorkItemID: "parity-fence-item-1"}: 2,
		{GenerationID: "gen-r1", WorkItemID: "parity-fence-item-2"}: 1,
	}
	if err := CheckHighWater(rows, want); err != nil {
		t.Fatalf("CheckHighWater(exact) = %v, want nil", err)
	}
	low := map[FencingHighWaterKey]int64{
		{GenerationID: "gen-r1", WorkItemID: "parity-fence-item-1"}: 1,
		{GenerationID: "gen-r1", WorkItemID: "parity-fence-item-2"}: 1,
	}
	if err := CheckHighWater(rows, low); err == nil {
		t.Fatalf("CheckHighWater(stale token 1 over committed 2) = nil, want an error")
	}
	if err := CheckHighWater(rows, map[FencingHighWaterKey]int64{}); err == nil {
		t.Fatalf("CheckHighWater(empty want over two rows) = nil, want an error")
	}
	if _, err := ReadFencingSnapshot(context.Background(), failingScanDB{}, "parity-fence-%"); err == nil ||
		!strings.Contains(err.Error(), "scan fencing snapshot") {
		t.Fatalf("ReadFencingSnapshot(failing scan) = %v, want the wrapped scan error", err)
	}
}

// TestFencingRehearsalRollbackTeardown proves the reversal deletes
// child-first by prefix and refuses to pass with residue left behind.
func TestFencingRehearsalRollbackTeardown(t *testing.T) {
	database := &rehearsalExecDB{affected: []int64{2, 1, 1}}
	deleted, err := TeardownRehearsalRows(context.Background(), database, "parity-fence-run")
	if err != nil {
		t.Fatalf("TeardownRehearsalRows() error = %v", err)
	}
	if deleted["fact_records"] != 2 || deleted["generation_commit_fences"] != 1 || deleted["scope_generations"] != 1 {
		t.Fatalf("deleted = %v, want one count per child-first table", deleted)
	}
	var deletes []string
	for _, stmt := range database.stmts {
		if strings.HasPrefix(stmt, "DELETE") {
			deletes = append(deletes, stmt)
		}
	}
	if len(deletes) != 3 {
		t.Fatalf("delete statements = %d, want 3 (facts, fences, generations)", len(deletes))
	}
	fact, fenced, generation := strings.Index(deletes[0], "fact_records"),
		strings.Index(deletes[1], "generation_commit_fences"),
		strings.Index(deletes[2], "scope_generations")
	if fact < 0 || fenced < 0 || generation < 0 {
		t.Fatalf("delete order = %v, want facts, fences, generations child-first", deletes)
	}
	for _, args := range database.args[:3] {
		if len(args) != 1 || !strings.Contains(args[0].(string), "parity-fence-run") {
			t.Fatalf("delete args = %v, want the single run-prefix pattern", database.args[:3])
		}
	}
	dirty := &rehearsalExecDB{residue: 3}
	if _, err := TeardownRehearsalRows(context.Background(), dirty, "parity-fence-run"); err == nil {
		t.Fatalf("TeardownRehearsalRows(residue 3) = nil, want the residue refusal")
	}
}
