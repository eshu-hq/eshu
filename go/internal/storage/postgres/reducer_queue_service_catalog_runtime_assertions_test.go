// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/servicecatalog"
)

// assertRuntimeFenceWrites checks every mock materialization call, not only the
// final call. A replay can write twice, but neither write may omit an instance.
func assertRuntimeFenceWrites(t *testing.T, writes []servicecatalog.ServiceMaterializationWrite, want int) {
	t.Helper()
	if len(writes) != want {
		t.Fatalf("materialization writes = %d (runtime rows %v), want %d complete calls", len(writes), runtimeRowCounts(writes), want)
	}
	for call, write := range writes {
		if write.ScopeID != runtimeFenceCatalogScope || write.ServiceID != serviceCatalogFenceServiceID || write.IntentID != runtimeFenceCatalogItem {
			t.Fatalf("write %d lineage = scope %q service %q intent %q", call, write.ScopeID, write.ServiceID, write.IntentID)
		}
		wantRows := map[string]string{
			"kubernetes:prod:workload-instance:checkout:prod":   "prod",
			"kubernetes:stage:workload-instance:checkout:stage": "stage",
		}
		if len(write.Runtime) != len(wantRows) {
			t.Fatalf("write %d runtime rows = %d, want %d", call, len(write.Runtime), len(wantRows))
		}
		for _, row := range write.Runtime {
			environment, ok := wantRows[row.Identity]
			if !ok || row.Retired || row.Payload["platform_kind"] != "kubernetes" || row.Payload["environment"] != environment || row.Payload["workload_ref"] != "workload-instance:checkout:"+environment {
				t.Fatalf("write %d has unexpected runtime row identity=%q retired=%v payload=%v", call, row.Identity, row.Retired, row.Payload)
			}
			delete(wantRows, row.Identity)
		}
		if len(wantRows) != 0 {
			t.Fatalf("write %d missing runtime identities: %v", call, wantRows)
		}
	}
}

// assertRuntimeFenceQueueSettled checks that no owed catalog replay or lease
// remains after all workers (and any manual replay) have finished.
func assertRuntimeFenceQueueSettled(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if status, _, attempts := readRuntimeFenceItem(t, ctx, db, runtimeFenceProducerItem); status != "succeeded" || attempts != 1 {
		t.Fatalf("producer terminal = %s attempts=%d, want succeeded at 1", status, attempts)
	}
	var status string
	var replayRequired bool
	var attempts int
	var owner sql.NullString
	var until sql.NullTime
	if err := db.QueryRowContext(ctx, `SELECT status, cross_scope_replay_required, attempt_count, lease_owner, claim_until
		FROM fact_work_items WHERE work_item_id = $1`, runtimeFenceCatalogItem,
	).Scan(&status, &replayRequired, &attempts, &owner, &until); err != nil {
		t.Fatalf("read catalog terminal: %v", err)
	}
	if status != "succeeded" || replayRequired || attempts != 1 || owner.Valid || until.Valid {
		t.Fatalf("catalog terminal = %s replay=%v attempts=%d lease=%v until=%v, want succeeded at 1 without replay or lease",
			status, replayRequired, attempts, owner.Valid, until.Valid)
	}
}
