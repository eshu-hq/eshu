// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// This file holds the live-Postgres fake types
// (containerImageIdentityAtomicLive*, containerImageIdentityPausingLiveTx,
// containerImageIdentityFailingLiveTx) that
// container_image_identity_writer_atomic_postgres_live_test.go drives against
// a real database connection. Split out because the combined file otherwise
// passed the repository's 500-line cap once the #5874 admission-aware
// ExecContainerImageIdentityClaimedAdmission methods were added.

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func assertContainerImageIdentityAtomicLiveCount(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	query string,
	want int,
	args ...any,
) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&got); err != nil {
		t.Fatalf("read atomic live row count: %v", err)
	}
	if got != want {
		t.Fatalf("atomic live row count = %d, want %d", got, want)
	}
}

type containerImageIdentityAtomicLiveBeginner struct {
	db   *sql.DB
	wrap func(*sql.Tx) ContainerImageIdentityTransaction
}

type containerImageIdentityAtomicLiveCutoverLookup struct {
	db *sql.DB
}

type containerImageIdentityAtomicLiveClaimedExecer struct {
	db *sql.DB
}

func (execer containerImageIdentityAtomicLiveClaimedExecer) ExecContainerImageIdentityClaimed(
	ctx context.Context,
	query string,
	args ...any,
) (int, bool, error) {
	rows, err := execer.db.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, false, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return 0, false, rows.Err()
	}
	var deleted int
	if err := rows.Scan(&deleted); err != nil {
		return 0, false, err
	}
	return deleted, true, rows.Err()
}

// ExecContainerImageIdentityClaimedAdmission is the admission-aware method
// (#5874) the writer's completed-cutover single-round-trip path actually
// calls; it scans the SECOND result column those queries now return
// (whether the woven-in container_image_identity_write_admission CAS
// admitted this pass) against a real Postgres connection.
func (execer containerImageIdentityAtomicLiveClaimedExecer) ExecContainerImageIdentityClaimedAdmission(
	ctx context.Context,
	query string,
	args ...any,
) (int, bool, bool, error) {
	rows, err := execer.db.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, false, false, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return 0, false, false, rows.Err()
	}
	var deleted int
	var admitted bool
	if err := rows.Scan(&deleted, &admitted); err != nil {
		return 0, false, false, err
	}
	return deleted, admitted, true, rows.Err()
}

func (lookup containerImageIdentityAtomicLiveCutoverLookup) ContainerImageIdentityCutoverExists(
	ctx context.Context,
	scopeID string,
	generationID string,
) (bool, error) {
	var exists bool
	err := lookup.db.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM container_image_identity_cutovers
    WHERE scope_id = $1
      AND generation_id = $2
)
`, scopeID, generationID).Scan(&exists)
	return exists, err
}

func (b *containerImageIdentityAtomicLiveBeginner) BeginContainerImageIdentityTx(
	ctx context.Context,
) (ContainerImageIdentityTransaction, error) {
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if b.wrap != nil {
		return b.wrap(tx), nil
	}
	return tx, nil
}

type containerImageIdentityPausingLiveTx struct {
	tx      *sql.Tx
	pauseAt int
	calls   int
	paused  chan<- struct{}
	release <-chan struct{}
}

func (tx *containerImageIdentityPausingLiveTx) ExecContext(
	ctx context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	tx.calls++
	if tx.calls == tx.pauseAt {
		close(tx.paused)
		select {
		case <-tx.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return tx.tx.ExecContext(ctx, query, args...)
}

func (tx *containerImageIdentityPausingLiveTx) ExecContainerImageIdentityClaimed(
	ctx context.Context,
	query string,
	args ...any,
) (int, bool, error) {
	rows, err := tx.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, false, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return 0, false, rows.Err()
	}
	var deleted int
	if err := rows.Scan(&deleted); err != nil {
		return 0, false, err
	}
	return deleted, true, rows.Err()
}

// ExecContainerImageIdentityClaimedAdmission mirrors
// containerImageIdentityAtomicLiveClaimedExecer's method of the same name
// (#5874) over the pausing tx's underlying *sql.Tx.
func (tx *containerImageIdentityPausingLiveTx) ExecContainerImageIdentityClaimedAdmission(
	ctx context.Context,
	query string,
	args ...any,
) (int, bool, bool, error) {
	rows, err := tx.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, false, false, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return 0, false, false, rows.Err()
	}
	var deleted int
	var admitted bool
	if err := rows.Scan(&deleted, &admitted); err != nil {
		return 0, false, false, err
	}
	return deleted, admitted, true, rows.Err()
}

func (tx *containerImageIdentityPausingLiveTx) Commit() error {
	return tx.tx.Commit()
}

func (tx *containerImageIdentityPausingLiveTx) Rollback() error {
	return tx.tx.Rollback()
}

type containerImageIdentityFailingLiveTx struct {
	tx     *sql.Tx
	failAt int
	calls  int
}

func (tx *containerImageIdentityFailingLiveTx) ExecContext(
	ctx context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	tx.calls++
	if tx.calls == tx.failAt {
		return nil, errors.New("synthetic mid-transaction chunk failure")
	}
	return tx.tx.ExecContext(ctx, query, args...)
}

func (tx *containerImageIdentityFailingLiveTx) Commit() error {
	return tx.tx.Commit()
}

func (tx *containerImageIdentityFailingLiveTx) Rollback() error {
	return tx.tx.Rollback()
}
