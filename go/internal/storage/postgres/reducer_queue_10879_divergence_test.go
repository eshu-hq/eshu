// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"
)

// TestReducerQueue11081StaleMirrorRowClearedByClaim proves a diverged mirror
// row (live-looking lease whose main row already finished without the
// mirror cleanup) is cleared by the same-key claim in-statement instead of
// deferring, erroring, or dead-lettering it (#11081: the claim heals the
// residue the previous-build writer left). A genuinely live sibling holder
// still defers through the main live-lease fence.
func TestReducerQueue11081StaleMirrorRowClearedByClaim(t *testing.T) {
	ctx := context.Background()
	conn, schemaName := setupLeaseTestSchema(t, ctx)
	now := time.Now().UTC()
	if seeded := seedReducerQueue10862Replay(t, ctx, conn, schemaName, now); seeded < 8000 {
		t.Fatalf("seed = %d, want >= 8000", seeded)
	}
	queue := leaseTestQueue(conn, "lease-diverge-owner")
	intent := claimOneSupplyChainItem(t, ctx, queue)

	var scopeID, generationID, domain, conflictDomain string
	if err := conn.conn.QueryRowContext(ctx,
		`SELECT scope_id, generation_id, domain, conflict_domain
		 FROM fact_work_items WHERE work_item_id = $1`,
		intent.IntentID).Scan(&scopeID, &generationID, &domain, &conflictDomain); err != nil {
		t.Fatalf("read claimed quad: %v", err)
	}

	// Diverge the mirror: finish the main row the way a main-only writer
	// would, leaving the live-looking lease row behind.
	if _, err := conn.ExecContext(ctx,
		`UPDATE fact_work_items
		 SET status = 'succeeded', lease_owner = NULL, claim_until = NULL,
		     last_attempt_at = NULL
		 WHERE work_item_id = $1`, intent.IntentID); err != nil {
		t.Fatalf("finish main row without mirror cleanup: %v", err)
	}

	// A sibling on the same conflict identity (NULL key coalesces to the
	// scope) with an old updated_at so it is the deterministic LIMIT 1
	// candidate: every seeded row is newer.
	old := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	if _, err := conn.ExecContext(ctx, `INSERT INTO fact_work_items (
	    work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
	    conflict_key, status, attempt_count, lease_owner, claim_until,
	    last_attempt_at, payload, created_at, updated_at
	) VALUES ('diverge-pending-B', $1, $2, 'reducer', $3, $4,
	    NULL, 'pending', 1, NULL, NULL,
	    NULL, '{}'::jsonb, $5, $5)`,
		scopeID, generationID, domain, conflictDomain, old); err != nil {
		t.Fatalf("insert diverged sibling: %v", err)
	}

	// The claim must clear the stale row and take the sibling in one
	// statement: the main UPDATE finds no live main holder, the chained
	// clear deletes the disowned lease row, and the mirror insert lands.
	got, claimed, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("diverged claim err = %v, want cleared claim nil", err)
	}
	if !claimed {
		t.Fatal("diverged claim returned no work, want the sibling")
	}
	if got.IntentID != "diverge-pending-B" {
		t.Fatalf("diverged claim took %q, want diverge-pending-B", got.IntentID)
	}

	// Atomicity: the sibling is claimed (not dead-lettered, not
	// half-claimed), the stale row is gone, and the mirror row is the
	// sibling's with the claiming owner.
	var status string
	if err := conn.conn.QueryRowContext(ctx,
		`SELECT status FROM fact_work_items WHERE work_item_id = 'diverge-pending-B'`,
	).Scan(&status); err != nil {
		t.Fatalf("read sibling status: %v", err)
	}
	if status != "claimed" {
		t.Fatalf("sibling status = %q, want claimed", status)
	}
	rows := liveLeaseRows(t, ctx, conn)
	if len(rows) != 1 || rows["diverge-pending-B"] != "lease-diverge-owner" {
		t.Fatalf("lease rows after clear = %v, want only the sibling row", rows)
	}

	// The finished A main row is untouched by the clearing claim beyond
	// its seeded terminal state.
	var aStatus string
	if err := conn.conn.QueryRowContext(ctx,
		`SELECT status FROM fact_work_items WHERE work_item_id = $1`,
		intent.IntentID).Scan(&aStatus); err != nil {
		t.Fatalf("read finished row status: %v", err)
	}
	if aStatus != "succeeded" {
		t.Fatalf("finished row status = %q, want succeeded", aStatus)
	}

	// A genuinely live sibling holder still defers through the main
	// live-lease fence (not the mirror): plant C on the same key and claim
	// with a second owner while the first queue holds B live.
	if _, err := conn.ExecContext(ctx, `INSERT INTO fact_work_items (
	    work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
	    conflict_key, status, attempt_count, lease_owner, claim_until,
	    last_attempt_at, payload, created_at, updated_at
	) VALUES ('diverge-pending-C', $1, $2, 'reducer', $3, $4,
	    NULL, 'pending', 1, NULL, NULL,
	    NULL, '{}'::jsonb, $5, $5)`,
		got.ScopeID, got.GenerationID, string(got.Domain), conflictDomain, old); err != nil {
		t.Fatalf("insert live-fenced sibling: %v", err)
	}
	other := leaseTestQueue(conn, "lease-diverge-other")
	otherGot, otherClaimed, err := other.Claim(ctx)
	if err != nil {
		t.Fatalf("live-fenced claim err = %v, want nil", err)
	}
	if otherClaimed && otherGot.IntentID == "diverge-pending-C" {
		t.Fatal("live-fenced claim took diverge-pending-C while B is live on the same key")
	}
	// C stays pending: the live holder fences the key but other keys flow.
	var cStatus string
	if err := conn.conn.QueryRowContext(ctx,
		`SELECT status FROM fact_work_items WHERE work_item_id = 'diverge-pending-C'`,
	).Scan(&cStatus); err != nil {
		t.Fatalf("read fenced sibling status: %v", err)
	}
	if cStatus != "pending" {
		t.Fatalf("fenced sibling status = %q, want pending", cStatus)
	}
}
