// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

type sqlConnExecQueryer struct {
	conn *sql.Conn
}

func (q sqlConnExecQueryer) QueryContext(
	ctx context.Context,
	query string,
	args ...any,
) (Rows, error) {
	return q.conn.QueryContext(ctx, query, args...)
}

func (q sqlConnExecQueryer) ExecContext(
	ctx context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	return q.conn.ExecContext(ctx, query, args...)
}

func TestContainerImageIdentityAckStatusAuthorizationHonorsTransactionBoundariesLive(
	t *testing.T,
) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	now := time.Date(2026, time.July, 30, 13, 0, 0, 0, time.UTC)
	const (
		owner = "capable-reducer-5854-reset"
	)

	for index := 1; index <= 7; index++ {
		scopeID := fmt.Sprintf("repository:5854-ack-reset-%d", index)
		generationID := fmt.Sprintf("generation:5854-ack-reset-%d", index)
		workItemID := fmt.Sprintf("ack-5854-reset-%d", index)
		seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
		seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, generationID)
		seedContainerImageIdentityAckWorkItem(
			t,
			ctx,
			db,
			workItemID,
			scopeID,
			generationID,
			owner,
			now.Add(time.Minute),
			now,
		)
		insertContainerImageIdentityCutoverMarker(t, ctx, db, scopeID, generationID)
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("reserve ACK attempt fence reset connection: %v", err)
	}
	connQueue := ReducerQueue{
		db:            sqlConnExecQueryer{conn: conn},
		LeaseOwner:    owner,
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}
	if err := connQueue.Ack(
		ctx,
		reducer.Intent{
			IntentID:     "ack-5854-reset-1",
			Domain:       reducer.DomainContainerImageIdentity,
			AttemptCount: 1,
			ClaimEpoch:   1,
		},
		reducer.Result{},
	); err != nil {
		t.Fatalf("autocommit attempt-bound ACK: %v", err)
	}
	result, legacyErr := conn.ExecContext(
		ctx,
		legacyContainerImageIdentityAckQuery,
		now,
		"ack-5854-reset-2",
		owner,
	)
	assertContainerImageIdentityLegacyAckRejected(t, result, legacyErr)

	rollbackTx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin attempt-bound ACK rollback transaction: %v", err)
	}
	rollbackQueue := ReducerQueue{
		db:            SQLTx{Tx: rollbackTx},
		LeaseOwner:    owner,
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}
	if err := rollbackQueue.Ack(
		ctx,
		reducer.Intent{
			IntentID:     "ack-5854-reset-3",
			Domain:       reducer.DomainContainerImageIdentity,
			AttemptCount: 1,
			ClaimEpoch:   1,
		},
		reducer.Result{},
	); err != nil {
		_ = rollbackTx.Rollback()
		t.Fatalf("attempt-bound ACK before rollback: %v", err)
	}
	if err := rollbackTx.Rollback(); err != nil {
		t.Fatalf("roll back attempt-bound ACK: %v", err)
	}
	result, legacyErr = conn.ExecContext(
		ctx,
		legacyContainerImageIdentityAckQuery,
		now,
		"ack-5854-reset-3",
		owner,
	)
	assertContainerImageIdentityLegacyAckRejected(t, result, legacyErr)

	commitTx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin attempt-bound ACK commit transaction: %v", err)
	}
	commitQueue := ReducerQueue{
		db:            SQLTx{Tx: commitTx},
		LeaseOwner:    owner,
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}
	if err := commitQueue.Ack(
		ctx,
		reducer.Intent{
			IntentID:     "ack-5854-reset-4",
			Domain:       reducer.DomainContainerImageIdentity,
			AttemptCount: 1,
			ClaimEpoch:   1,
		},
		reducer.Result{},
	); err != nil {
		_ = commitTx.Rollback()
		t.Fatalf("attempt-bound ACK before commit: %v", err)
	}
	if err := commitTx.Commit(); err != nil {
		t.Fatalf("commit attempt-bound ACK: %v", err)
	}
	result, legacyErr = conn.ExecContext(
		ctx,
		legacyContainerImageIdentityAckQuery,
		now,
		"ack-5854-reset-5",
		owner,
	)
	assertContainerImageIdentityLegacyAckRejected(t, result, legacyErr)

	savepointTx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin attempt-bound ACK savepoint transaction: %v", err)
	}
	defer func() { _ = savepointTx.Rollback() }()
	if _, err := savepointTx.ExecContext(ctx, "SAVEPOINT before_capable_ack"); err != nil {
		t.Fatalf("create attempt-bound ACK savepoint: %v", err)
	}
	savepointQueue := ReducerQueue{
		db:            SQLTx{Tx: savepointTx},
		LeaseOwner:    owner,
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}
	if err := savepointQueue.Ack(
		ctx,
		reducer.Intent{
			IntentID:     "ack-5854-reset-6",
			Domain:       reducer.DomainContainerImageIdentity,
			AttemptCount: 1,
			ClaimEpoch:   1,
		},
		reducer.Result{},
	); err != nil {
		t.Fatalf("attempt-bound ACK inside savepoint: %v", err)
	}
	if _, err := savepointTx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT before_capable_ack"); err != nil {
		t.Fatalf("roll back attempt-bound ACK savepoint: %v", err)
	}
	result, legacyErr = savepointTx.ExecContext(
		ctx,
		legacyContainerImageIdentityAckQuery,
		now,
		"ack-5854-reset-6",
		owner,
	)
	assertContainerImageIdentityLegacyAckRejected(t, result, legacyErr)
	if err := savepointTx.Rollback(); err != nil {
		t.Fatalf("roll back legacy-rejected savepoint transaction: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("return ACK attempt fence connection to pool: %v", err)
	}

	result, legacyErr = db.ExecContext(
		ctx,
		legacyContainerImageIdentityAckQuery,
		now,
		"ack-5854-reset-7",
		owner,
	)
	assertContainerImageIdentityLegacyAckRejected(t, result, legacyErr)

	for workItemID, wantStatus := range map[string]string{
		"ack-5854-reset-1": "succeeded",
		"ack-5854-reset-2": "running",
		"ack-5854-reset-3": "running",
		"ack-5854-reset-4": "succeeded",
		"ack-5854-reset-5": "running",
		"ack-5854-reset-6": "running",
		"ack-5854-reset-7": "running",
	} {
		wantOwner := owner
		if wantStatus == "succeeded" {
			wantOwner = ""
		}
		assertContainerImageIdentityAckWorkItemState(
			t,
			ctx,
			db,
			workItemID,
			wantStatus,
			wantOwner,
		)
	}
}
