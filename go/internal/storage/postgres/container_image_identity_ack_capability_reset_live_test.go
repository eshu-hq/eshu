// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

type sqlConnExecQueryer struct {
	conn *sql.Conn
}

func (q sqlConnExecQueryer) QueryContext(
	ctx context.Context,
	query string,
	args ...any,
) (db.Rows, error) {
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
	database := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	// Live clock: the ACK fence requires claim_until > clock_timestamp().
	now := time.Now().UTC().Truncate(time.Microsecond)
	const (
		owner = "capable-reducer-5854-reset"
	)
	// stampAttemptBoundIntent stamps a seeded row the way a real claim would
	// (#7691) and returns the attempt-bound intent the capable ACK must
	// carry: the fence matches last_attempt_at, a live claim_until, and the
	// post-stamp claim epoch, so a hand-built intent without them is
	// legitimately rejected.
	stampAttemptBoundIntent := func(workItemID string) reducer.Intent {
		claimedAt, claimEpoch := stampContainerImageIdentityAckClaim(t, ctx, database, workItemID)
		return reducer.Intent{
			IntentID:     workItemID,
			Domain:       reducer.DomainContainerImageIdentity,
			AttemptCount: 1,
			ClaimEpoch:   claimEpoch,
			ClaimedAt:    &claimedAt,
		}
	}

	for index := 1; index <= 7; index++ {
		scopeID := fmt.Sprintf("repository:5854-ack-reset-%d", index)
		generationID := fmt.Sprintf("generation:5854-ack-reset-%d", index)
		workItemID := fmt.Sprintf("ack-5854-reset-%d", index)
		seedContainerImageIdentityAckScope(t, ctx, database, scopeID)
		seedContainerImageIdentityAckGeneration(t, ctx, database, scopeID, generationID)
		seedContainerImageIdentityAckWorkItem(
			t,
			ctx,
			database,
			workItemID,
			scopeID,
			generationID,
			owner,
			now.Add(time.Minute),
			now,
		)
		insertContainerImageIdentityCutoverMarker(t, ctx, database, scopeID, generationID)
	}

	conn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("reserve ACK attempt fence reset connection: %v", err)
	}
	connQueue := ReducerQueue{
		database:      sqlConnExecQueryer{conn: conn},
		LeaseOwner:    owner,
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}
	if err := connQueue.Ack(
		ctx,
		stampAttemptBoundIntent("ack-5854-reset-1"),
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
	assertContainerImageIdentityLegacyAckFenced(
		t, ctx, conn, "ack-5854-reset-2", result, legacyErr, 1, "pending",
	)

	rollbackTx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin attempt-bound ACK rollback transaction: %v", err)
	}
	rollbackQueue := ReducerQueue{
		database:      SQLTx{Tx: rollbackTx},
		LeaseOwner:    owner,
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}
	reset3 := stampAttemptBoundIntent("ack-5854-reset-3")
	if err := rollbackQueue.Ack(ctx, reset3, reducer.Result{}); err != nil {
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
	// The stamp advanced the epoch to 2; the rolled-back ACK left it there.
	assertContainerImageIdentityLegacyAckFenced(
		t, ctx, conn, "ack-5854-reset-3", result, legacyErr, 2, "pending",
	)

	commitTx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin attempt-bound ACK commit transaction: %v", err)
	}
	commitQueue := ReducerQueue{
		database:      SQLTx{Tx: commitTx},
		LeaseOwner:    owner,
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}
	if err := commitQueue.Ack(
		ctx,
		stampAttemptBoundIntent("ack-5854-reset-4"),
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
	assertContainerImageIdentityLegacyAckFenced(
		t, ctx, conn, "ack-5854-reset-5", result, legacyErr, 1, "pending",
	)

	savepointTx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin attempt-bound ACK savepoint transaction: %v", err)
	}
	defer func() { _ = savepointTx.Rollback() }()
	if _, err := savepointTx.ExecContext(ctx, "SAVEPOINT before_capable_ack"); err != nil {
		t.Fatalf("create attempt-bound ACK savepoint: %v", err)
	}
	savepointQueue := ReducerQueue{
		database:      SQLTx{Tx: savepointTx},
		LeaseOwner:    owner,
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}
	reset6 := stampAttemptBoundIntent("ack-5854-reset-6")
	if err := savepointQueue.Ack(ctx, reset6, reducer.Result{}); err != nil {
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
	// Read through the transaction: the fence is visible in-tx and proves
	// the legacy statement succeeds rather than poisoning the tx, while the
	// final rollback below restores the running row.
	assertContainerImageIdentityLegacyAckFenced(
		t, ctx, savepointTx, "ack-5854-reset-6", result, legacyErr, 2, "pending",
	)
	if err := savepointTx.Rollback(); err != nil {
		t.Fatalf("roll back legacy-fenced savepoint transaction: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("return ACK attempt fence connection to pool: %v", err)
	}

	result, legacyErr = database.ExecContext(
		ctx,
		legacyContainerImageIdentityAckQuery,
		now,
		"ack-5854-reset-7",
		owner,
	)
	assertContainerImageIdentityLegacyAckFenced(
		t, ctx, database, "ack-5854-reset-7", result, legacyErr, 1, "pending",
	)

	for workItemID, want := range map[string]struct {
		status string
		owner  string
	}{
		"ack-5854-reset-1": {"succeeded", ""},
		"ack-5854-reset-2": {"pending", ""},
		"ack-5854-reset-3": {"pending", ""},
		"ack-5854-reset-4": {"succeeded", ""},
		"ack-5854-reset-5": {"pending", ""},
		"ack-5854-reset-6": {"running", owner},
		"ack-5854-reset-7": {"pending", ""},
	} {
		assertContainerImageIdentityAckWorkItemState(
			t,
			ctx,
			database,
			workItemID,
			want.status,
			want.owner,
		)
	}
}
