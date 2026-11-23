// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"reflect"
	"sort"
	"strings"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// coverRow is one supply_chain_impact item at a holder's active generation.
// merge is jsonb merged into the seeded payload ({} keeps it unstamped).
type coverRow struct {
	status       string
	attempts     int
	failureClass string
	merge        string
}

// coverShape is one arm of the #10972 cover rule: the holder's rows, the
// fan-out's stampOut, and whether the prefilter must drop the holder.
type coverShape struct {
	scope    string
	rows     []coverRow
	stampOut []string
	dropped  bool
}

// coverNonCounting is a non-counting retry class, from the shipped set the
// claim's attempt CASE reads, so the matrix never hand-copies it.
func coverNonCounting() string { return postgres.NonCountingReducerRetryFailureClasses()[0] }

func coverPending(merge string) coverRow { return coverRow{status: "pending", merge: merge} }

const (
	coverStampA  = `{"fanout_origin": {"scope_id": "scope:a", "cves": ["` + widenCVEA + `"]}}`
	coverStampAB = `{"fanout_origin": {"scope_id": "scope:a", "cves": ["` + widenCVEA + `", "` + widenCVEB + `"]}}`
	coverStampB  = `{"fanout_origin": {"scope_id": "scope:b", "cves": ["` + widenCVEB + `"]}}`
)

// coverShapes is the matrix. Every single-row shape is also a lockstep shape
// (supply_chain_impact_fanout_queued_lockstep_live_test.go).
func coverShapes() []coverShape {
	b := []string{widenCVEB}
	return []coverShape{
		{"c-no-stamp", []coverRow{coverPending(`{}`)}, b, true},
		{"c-json-null-stamp", []coverRow{coverPending(`{"fanout_origin": null}`)}, b, true},
		{"c-string-stamp", []coverRow{coverPending(`{"fanout_origin": "x"}`)}, b, true},
		{"c-setless-stamp", []coverRow{coverPending(`{"fanout_origin": {"scope_id": "scope:a"}}`)}, b, true},
		{"c-empty-cves", []coverRow{coverPending(`{"fanout_origin": {"scope_id": "scope:a", "cves": []}}`)}, b, true},
		{"c-sentinel", []coverRow{coverPending(`{"fanout_origin": {"scope_id": "scope:a", "cves": ["*"]}}`)}, b, true},
		{"c-owed", []coverRow{coverPending(`{"fanout_owed": true, "fanout_origin": {"scope_id": "scope:a", "cves": ["` + widenCVEA + `"]}}`)}, b, true},
		{"c-owed-false", []coverRow{coverPending(`{"fanout_owed": false, "fanout_origin": {"scope_id": "scope:a", "cves": ["` + widenCVEA + `"]}}`)}, b, false},
		{"c-owed-string", []coverRow{coverPending(`{"fanout_owed": "true", "fanout_origin": {"scope_id": "scope:a", "cves": ["` + widenCVEA + `"]}}`)}, b, false},
		{"c-pending-attempt-1", []coverRow{{status: "pending", attempts: 1, merge: coverStampA}}, b, true},
		{"c-retrying-counting", []coverRow{{status: "retrying", attempts: 1, failureClass: "synthetic_counting", merge: coverStampA}}, b, true},
		// The arm the first predicate got wrong: a non-counting retry keeps its
		// count, so it is claimed at attempt 1 and runs scoped on {A}.
		{"c-retrying-non-counting", []coverRow{{status: "retrying", attempts: 1, failureClass: coverNonCounting(), merge: coverStampA}}, b, false},
		{"c-covered-by-set", []coverRow{coverPending(coverStampAB)}, b, true},
		{"c-narrower-than-change", []coverRow{coverPending(coverStampA)}, []string{widenCVEA, widenCVEB}, false},
		{"c-empty-change", []coverRow{coverPending(coverStampA)}, nil, false},
		{"c-sentinel-change", []coverRow{coverPending(coverStampA)}, []string{supplychaincore.FanOutStampSentinel}, false},
		{"c-numeric-cves", []coverRow{coverPending(`{"fanout_origin": {"scope_id": "scope:a", "cves": [1]}}`)}, b, false},
		// Two rows, the second's owed arm absent (NULL in a bare comparison):
		// bool_and ignores NULL rows, so a NULL-unsafe predicate drops this.
		{"c-two-one-null-arms", []coverRow{coverPending(coverStampB), coverPending(coverStampA)}, b, false},
		{"c-two-both-covered", []coverRow{coverPending(`{}`), coverPending(coverStampB)}, b, true},
		{"c-two-one-claimed", []coverRow{coverPending(`{}`), {status: "claimed", merge: `{}`}}, b, false},
	}
}

// coverItemID is the stored id of a shape's i-th row.
func coverItemID(scope string, i int) string {
	id := fanOutItemID(scope, fanOutGen(scope, "g1"))
	if i > 0 {
		id += "_second"
	}
	return id
}

// seedCoverShapes seeds every shape at its own scope's active generation.
func seedCoverShapes(t *testing.T, ctx context.Context, db *sql.DB, shapes []coverShape) {
	t.Helper()
	for _, shape := range shapes {
		fanOutSeedScope(t, ctx, db, shape.scope, "g1")
		for i, row := range shape.rows {
			id := coverItemID(shape.scope, i)
			if i == 0 {
				fanOutSeedItem(t, ctx, db, shape.scope, "g1")
			} else if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, conflict_domain, conflict_key,
  status, attempt_count, payload, created_at, updated_at)
VALUES ($1, $2, $3, 'reducer', 'supply_chain_impact', 'scope', $1, 'pending', 0,
  jsonb_build_object('entity_key', $1::text), now() - interval '1 minute', now() - interval '1 minute')`,
				id, shape.scope, fanOutGen(shape.scope, "g1")); err != nil {
				t.Fatalf("seed %s: %v", id, err)
			}
			res, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = $2, attempt_count = $3, failure_class = NULLIF($4, ''), payload = payload || $5::jsonb,
    visible_at = now() - interval '1 second',
    lease_owner = CASE WHEN $2 = 'claimed' THEN 'w' END,
    claim_until = CASE WHEN $2 = 'claimed' THEN now() + interval '1 hour' END,
    last_attempt_at = CASE WHEN $2 = 'claimed' THEN now() END
WHERE work_item_id = $1`, id, row.status, row.attempts, row.failureClass, row.merge)
			if err != nil {
				t.Fatalf("settle %s: %v", id, err)
			}
			if n, err := res.RowsAffected(); err != nil || n != 1 {
				t.Fatalf("settle %s updated %d rows (err %v)", id, n, err)
			}
		}
	}
}

// coverOrigin is the fan-out origin whose canonical stamp set is stampOut.
func coverOrigin(stampOut []string) supplychaincore.FanOutOrigin {
	return supplychaincore.FanOutOrigin{SourceScopeID: "scope:cover", SourceGenerationID: "gen-cover", SourceIntentID: "intent-cover", CVEIDs: stampOut}
}

// TestFindQueuedHolderScopesCoverMatrixLive reads the shipped prefilter once
// per shape with that shape's stampOut and pins the drop decision of every arm.
func TestFindQueuedHolderScopesCoverMatrixLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	shapes := coverShapes()
	seedCoverShapes(t, ctx, db, shapes)
	store := fanOutLiveStore(db)
	for _, shape := range shapes {
		got, err := store.FindQueuedHolderScopes(ctx, []string{shape.scope}, shape.stampOut)
		if err != nil {
			t.Fatalf("%s: FindQueuedHolderScopes() error = %v", shape.scope, err)
		}
		if dropped := len(got) == 1; dropped != shape.dropped || len(got) > 1 {
			t.Errorf("%s: prefilter returned %v, want dropped=%v", shape.scope, got, shape.dropped)
		}
	}
}

// coverNullUnsafeQuery is the seeded violation: the shipped prefilter with the
// first predicate's row condition, bare, in place of the NULL-safe one.
func coverNullUnsafeQuery(t *testing.T) string {
	t.Helper()
	const boundary = "HAVING bool_and("
	shipped := postgres.FindQueuedSupplyChainImpactHoldersQueryForTest
	cut := strings.Index(shipped, boundary)
	if cut < 0 {
		t.Fatal("the shipped prefilter has no bool_and HAVING to seed against")
	}
	return shipped[:cut] + boundary + `
  work.status IN ('pending', 'retrying')
  AND (
       COALESCE(jsonb_typeof(work.payload->'fanout_origin'), 'absent') <> 'object'
    OR COALESCE(jsonb_typeof(work.payload->'fanout_origin'->'cves'), 'absent') <> 'array'
    OR jsonb_array_length(work.payload->'fanout_origin'->'cves') = 0
    OR work.payload->'fanout_origin'->'cves' @> '["*"]'::jsonb
    OR work.payload->'fanout_owed' = 'true'::jsonb
    OR work.attempt_count + 1 > 1
    OR ($3::boolean AND work.payload->'fanout_origin'->'cves' @> $2::jsonb)
  ))
ORDER BY work.scope_id
`
}

// TestFindQueuedHolderScopesNullSafeRowConditionSeededRedGreenLive: on the
// two-row shape whose second row's owed arm is absent, the NULL-unsafe form
// drops the holder (RED: bool_and ignored the NULL row) and the shipped form
// keeps it (GREEN).
func TestFindQueuedHolderScopesNullSafeRowConditionSeededRedGreenLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	var shape coverShape
	for _, candidate := range coverShapes() {
		if candidate.scope == "c-two-one-null-arms" {
			shape = candidate
		}
	}
	seedCoverShapes(t, ctx, db, []coverShape{shape})
	var unsafe []string
	rows, err := db.QueryContext(ctx, coverNullUnsafeQuery(t), []string{shape.scope}, `["`+widenCVEB+`"]`, true)
	if err != nil {
		t.Fatalf("seeded NULL-unsafe read: %v", err)
	}
	for rows.Next() {
		var scope string
		if err := rows.Scan(&scope); err != nil {
			t.Fatal(err)
		}
		unsafe = append(unsafe, scope)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(unsafe, []string{shape.scope}) {
		t.Fatalf("NULL-unsafe form returned %v, want it to drop %s: the seeded violation is inert", unsafe, shape.scope)
	}
	shipped, err := fanOutLiveStore(db).FindQueuedHolderScopes(ctx, []string{shape.scope}, shape.stampOut)
	if err != nil || len(shipped) != 0 {
		t.Fatalf("shipped prefilter = %v err=%v, want the holder kept", shipped, err)
	}
}

// coverScopeVersions returns the xmin of every row of one scope.
func coverScopeVersions(t *testing.T, ctx context.Context, db *sql.DB, scope string) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, `SELECT work_item_id || '=' || xmin::text FROM fact_work_items WHERE scope_id = $1 ORDER BY work_item_id`, scope)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	return out
}

// TestFindQueuedHolderScopesReturnedHoldersGetNoRowVersionFromTheReopenLive is
// the differential that replaces the status-only one: for every seeded shape,
// a holder the prefilter returns gets no row version from the shipped reopen
// for the same origin and reports pending_noop, and a kept holder whose rows
// are all queued is widened.
func TestFindQueuedHolderScopesReturnedHoldersGetNoRowVersionFromTheReopenLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	shapes := coverShapes()
	seedCoverShapes(t, ctx, db, shapes)
	seedQueuedHolderStates(t, ctx, db)
	for _, state := range queuedHolderStates {
		// The status shapes carry no stamp; read them with an unbounded change.
		switch state.scope {
		case "h-no-item", "h-failed-only":
			continue // the reopen enqueues there; nothing is returned for them
		}
		shapes = append(shapes, coverShape{scope: state.scope})
	}
	sort.Slice(shapes, func(i, j int) bool { return shapes[i].scope < shapes[j].scope })
	store := fanOutLiveStore(db)
	returned := 0
	for _, shape := range shapes {
		got, err := store.FindQueuedHolderScopes(ctx, []string{shape.scope}, shape.stampOut)
		if err != nil {
			t.Fatal(err)
		}
		before := coverScopeVersions(t, ctx, db, shape.scope)
		reopen, err := store.ReopenHolderDetailed(ctx, shape.scope, coverOrigin(shape.stampOut))
		if err != nil {
			t.Fatalf("%s: reopen: %v", shape.scope, err)
		}
		after := coverScopeVersions(t, ctx, db, shape.scope)
		if len(got) == 1 {
			returned++
			if !reflect.DeepEqual(before, after) || reopen.Outcome != supplychaincore.FanOutPendingNoop {
				t.Errorf("%s: returned by the prefilter, but the reopen = %+v and versions %v -> %v", shape.scope, reopen, before, after)
			}
			continue
		}
		if len(shape.rows) > 0 && coverAllQueued(shape.rows) && reopen.Outcome != supplychaincore.FanOutStampWidened {
			t.Errorf("%s: kept and queued, reopen = %+v, want stamp_widened", shape.scope, reopen)
		}
	}
	if returned == 0 {
		t.Fatal("the prefilter returned no holder: the differential proves nothing")
	}
}

func coverAllQueued(rows []coverRow) bool {
	for _, row := range rows {
		if row.status != "pending" && row.status != "retrying" {
			return false
		}
	}
	return true
}
