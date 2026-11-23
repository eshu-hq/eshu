// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/scope/completion"
)

// completionConsumerSnapshot is every column the completion fan-out can change
// on a consumer row, so a test can prove a row was left byte-for-byte alone.
type completionConsumerSnapshot struct {
	Status     string
	ReopenedAt sql.NullTime
	UpdatedAt  time.Time
	Replay     bool
	Attempts   int
}

func readCompletionConsumerSnapshots(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	ids ...string,
) map[string]completionConsumerSnapshot {
	t.Helper()
	out := make(map[string]completionConsumerSnapshot, len(ids))
	for _, id := range ids {
		var snapshot completionConsumerSnapshot
		if err := db.QueryRowContext(ctx, `
SELECT status, reopened_at, updated_at, cross_scope_replay_required, attempt_count
FROM fact_work_items WHERE work_item_id = $1
`, id).Scan(
			&snapshot.Status, &snapshot.ReopenedAt, &snapshot.UpdatedAt, &snapshot.Replay, &snapshot.Attempts,
		); err != nil {
			t.Fatalf("snapshot completion consumer %s: %v", id, err)
		}
		out[id] = snapshot
	}
	return out
}

// TestCrossScopeCompletionFanoutLeavesManifestMarkersSucceededLive pins the
// completion contract (#10972): A completion event for producer P schedules
// exactly the active-generation items of P's consumer domains whose handler
// reads P's output; a manifest marker is never scheduled or flagged. A marker
// (payload trigger=manifest_dependency) reads no CI/CD or identity evidence
// and writes no finding, so re-pending a succeeded one or flagging a claimed
// one only re-runs a pass that cannot change any finding.
func TestCrossScopeCompletionFanoutLeavesManifestMarkersSucceededLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	now := time.Now().UTC().Truncate(time.Microsecond)
	stale := now.Add(-time.Hour)
	const (
		scopeID    = "repository:10972-markers"
		generation = "generation:10972-markers"
		owner      = "markers-owner"

		markerSucceeded = "reducer_10972_marker_succeeded"
		markerClaimed   = "reducer_10972_marker_claimed"
		findings        = "reducer_10972_findings_succeeded"
	)
	seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
	seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, generation)
	if _, err := db.ExecContext(ctx, `
UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1
`, scopeID, generation); err != nil {
		t.Fatalf("activate marker scope: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    conflict_domain, conflict_key, lease_owner, claim_until,
    payload, created_at, updated_at
)
SELECT item.id, $1, $2, 'reducer', 'supply_chain_impact', item.status,
       'intent', item.id,
       CASE WHEN item.status = 'claimed' THEN $3::text END,
       CASE WHEN item.status = 'claimed' THEN $4::timestamptz END,
       item.payload::jsonb, $5, $5
FROM (VALUES
    ('reducer_10972_marker_succeeded', 'succeeded', '{"trigger":"manifest_dependency"}'),
    ('reducer_10972_marker_claimed',   'claimed',   '{"trigger":"manifest_dependency"}'),
    ('reducer_10972_findings_succeeded', 'succeeded', '{}')
) AS item(id, status, payload)
`, scopeID, generation, owner, now.Add(time.Minute), stale); err != nil {
		t.Fatalf("seed marker and findings consumers: %v", err)
	}
	all := []string{markerSucceeded, markerClaimed, findings}
	before := readCompletionConsumerSnapshots(t, ctx, db, all...)

	store := completionstore.NewCrossScopeCompletionStore(SQLDB{DB: db})
	store.Now = func() time.Time { return now }
	fanout := func(label string) reducer.CrossScopeCompletionResult {
		t.Helper()
		insertCrossScopeCompletionEvent(
			t, ctx, db, reducer.DomainCICDRunCorrelation,
			"pending", "", time.Time{}, 0, now.Add(-time.Second),
		)
		lease, ok, err := store.Claim(ctx, owner, time.Minute)
		if err != nil || !ok {
			t.Fatalf("%s: claim ci_cd completion event = %+v ok=%t err=%v", label, lease, ok, err)
		}
		result, err := store.Fanout(ctx, lease, 500)
		if err != nil {
			t.Fatalf("%s: fanout ci_cd completion event: %v", label, err)
		}
		return result
	}

	first := fanout("first")
	if first.IntentsEnqueued != 1 {
		t.Fatalf("first fanout = %+v, want exactly the findings item scheduled", first)
	}
	afterFirst := readCompletionConsumerSnapshots(t, ctx, db, all...)
	for _, id := range []string{markerSucceeded, markerClaimed} {
		if !reflect.DeepEqual(afterFirst[id], before[id]) {
			t.Fatalf("manifest marker %s changed by a completion fan-out: before=%+v after=%+v", id, before[id], afterFirst[id])
		}
	}
	if got := afterFirst[findings]; got.Status != "pending" || got.Replay || !got.ReopenedAt.Valid {
		t.Fatalf("findings item = %+v, want re-pended as before the marker fix", got)
	}

	second := fanout("second")
	if second.IntentsEnqueued != 0 {
		t.Fatalf("second identical fanout = %+v, want nothing scheduled", second)
	}
	if afterSecond := readCompletionConsumerSnapshots(t, ctx, db, all...); !reflect.DeepEqual(afterSecond, afterFirst) {
		t.Fatalf("second identical fan-out changed consumers: first=%+v second=%+v", afterFirst, afterSecond)
	}
}
