// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package actionsstore_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason Actions delivery claim skips held PostgreSQL row locks; local race proof and seeded missing-SKIP-LOCKED regression pass.
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/webhook"
)

func TestActionsClaimSkipsHeldRowLocksLive(t *testing.T) {
	store, database := localActionsStore(t)
	at := time.Date(2026, time.October, 26, 4, 0, 0, 0, time.UTC)
	for index := range 10 {
		_, err := store.Store(t.Context(), webhook.ActionsTrigger{
			DeliveryID: fmt.Sprintf("locked-%02d", index), EventKind: "workflow_run",
			Action: "completed", RepositoryID: "42", Repository: "example/repository",
			RunID: fmt.Sprint(index + 1),
		}, at)
		if err != nil {
			t.Fatal(err)
		}
	}
	holdContext, stopHolding := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(stopHolding)
	transaction, err := database.BeginTx(holdContext, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := transaction.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			t.Errorf("rollback lock-holder transaction: %v", err)
		}
	})
	rows, err := transaction.QueryContext(t.Context(), `SELECT delivery_id
 FROM cicd_actions_deliveries ORDER BY delivery_id LIMIT 5 FOR UPDATE`)
	if err != nil {
		t.Fatal(err)
	}
	locked := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		locked[id] = true
	}
	readErr := rows.Err()
	closeErr := rows.Close()
	if readErr != nil || closeErr != nil || len(locked) != 5 {
		t.Fatalf("lock-holder rows=%d read=%v close=%v", len(locked), readErr, closeErr)
	}
	// The first transaction remains open. Without SKIP LOCKED the production
	// claim waits on its first row until this independent context expires.
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	claimed, err := store.Claim(ctx, "independent-worker", at, 5, time.Minute)
	if err != nil || len(claimed) != 5 {
		t.Fatalf("claim behind held row locks count=%d error=%v", len(claimed), err)
	}
	for _, claim := range claimed {
		if locked[claim.DeliveryID] {
			t.Errorf("production claim returned a row held by another transaction")
		}
	}
	if _, err := transaction.ExecContext(ctx, `SELECT delivery_id FROM cicd_actions_deliveries WHERE delivery_id=$1 FOR UPDATE`, claimed[0].DeliveryID); err != nil {
		t.Fatal(err)
	}
	// One expired claim is now locked. The other four still make progress.
	reapAt := at.Add(6 * time.Minute)
	first, err := store.Reap(ctx, reapAt, 5)
	if err != nil || first.Requeued != 4 || first.Exhausted != 0 {
		t.Fatalf("reap behind held claim=%+v err=%v", first, err)
	}
	if err := transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
	last, err := store.Reap(ctx, reapAt, 5)
	if err != nil || last.Requeued != 1 || last.Exhausted != 0 {
		t.Fatalf("reap after unlock=%+v err=%v", last, err)
	}
}
