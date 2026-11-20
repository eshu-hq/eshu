// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"
)

// TestReducerQueue10879StaleMirrorRowDefersClaim proves a diverged mirror
// row (live-looking lease whose main row already finished without the
// mirror cleanup) defers a same-key claim through the lease-unique mapper
// instead of erroring or dead-lettering it, and that reconciling the stale
// row lets the claim through.
func TestReducerQueue10879StaleMirrorRowDefersClaim(t *testing.T) {
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

	// The claim must defer (no work, no error): the main UPDATE finds no
	// live main holder, and the mirror INSERT hits the stale lease row.
	got, claimed, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("diverged claim err = %v, want deferred nil", err)
	}
	if claimed {
		t.Fatalf("diverged claim took %q, want deferral", got.IntentID)
	}

	// Atomicity: the sibling is still pending (not dead-lettered, not
	// half-claimed) and the stale mirror row is untouched.
	var status string
	if err := conn.conn.QueryRowContext(ctx,
		`SELECT status FROM fact_work_items WHERE work_item_id = 'diverge-pending-B'`,
	).Scan(&status); err != nil {
		t.Fatalf("read sibling status: %v", err)
	}
	if status != "pending" {
		t.Fatalf("sibling status = %q, want pending", status)
	}
	if rows := liveLeaseRows(t, ctx, conn); len(rows) != 1 || rows[intent.IntentID] == "" {
		t.Fatalf("lease rows after defer = %v, want only the stale row", rows)
	}

	// Reconcile the stale row; the deferred claim must now go through.
	if _, err := conn.ExecContext(ctx,
		`DELETE FROM fact_work_item_leases WHERE work_item_id = $1`,
		intent.IntentID); err != nil {
		t.Fatalf("reconcile stale lease row: %v", err)
	}
	recovered, claimed, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("post-reconcile claim err: %v", err)
	}
	if !claimed {
		t.Fatal("post-reconcile claim returned no work, want the sibling")
	}
	if recovered.IntentID != "diverge-pending-B" {
		t.Fatalf("post-reconcile claim took %q, want diverge-pending-B", recovered.IntentID)
	}
	if got := liveLeaseRows(t, ctx, conn)["diverge-pending-B"]; got != "lease-diverge-owner" {
		t.Fatalf("sibling lease owner = %q, want lease-diverge-owner", got)
	}
}
