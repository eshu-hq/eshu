// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestPostgresContainerImageIdentityFirstCutoverRetriesExistingLegacyUpsertWithoutDeadlock(
	t *testing.T,
) {
	db := openContainerImageIdentityLivePostgres(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	seedContainerImageIdentityLiveParents(t, ctx, db)

	write := containerImageIdentityAtomicLiveWrite(
		"existing-legacy-upsert",
		2,
		time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC),
	)
	cleanupContainerImageIdentityAtomicLiveWrite(t, db, write)
	t.Cleanup(func() {
		cleanupContainerImageIdentityAtomicLiveWrite(t, db, write)
	})
	legacyFactIDs := write.LegacyFactIDs
	if err := reducerBatchInsertFacts(
		ctx,
		db,
		[]reducerFactRow{
			containerImageIdentityLegacyLiveRow(legacyFactIDs[0], 1, false),
			containerImageIdentityLegacyLiveRow(legacyFactIDs[1], 1, false),
		},
	); err != nil {
		t.Fatalf("seed existing legacy rows: %v", err)
	}

	markerCommitted := make(chan struct{})
	releaseMarker := make(chan struct{})
	writer := PostgresContainerImageIdentityWriter{
		DB: db,
		Beginner: &containerImageIdentityAtomicLiveBeginner{
			db: db,
			wrap: func(tx *sql.Tx) ContainerImageIdentityTransaction {
				return &containerImageIdentityPauseAfterLiveTx{
					tx: tx,
					// #5874: the admission CAS is now this transaction's
					// FIRST ExecContext call, shifting the cutover fence
					// (the marker this test needs committed-but-uncommitted
					// before releasing) to position 2.
					pauseAt: 2,
					paused:  markerCommitted,
					release: releaseMarker,
				}
			},
		},
	}
	writerDone := make(chan error, 1)
	go func() {
		_, err := writer.WriteContainerImageIdentityDecisions(ctx, write)
		writerDone <- err
	}()
	select {
	case <-markerCommitted:
	case <-ctx.Done():
		t.Fatal("first-cutover writer did not acquire the marker fence")
	}

	oldWriterDone := make(chan error, 1)
	go func() {
		oldWriterDone <- reducerBatchInsertFacts(
			ctx,
			db,
			[]reducerFactRow{
				containerImageIdentityLegacyLiveRow(legacyFactIDs[1], 2, false),
				containerImageIdentityLegacyLiveRow(legacyFactIDs[0], 2, false),
			},
		)
	}()
	select {
	case err := <-oldWriterDone:
		t.Fatalf("old writer returned before marker release: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(releaseMarker)
	var firstErr error
	select {
	case firstErr = <-writerDone:
	case <-ctx.Done():
		t.Fatal("first-cutover writer did not reject its busy legacy row")
	}
	var retryable interface{ Retryable() bool }
	if !errors.As(firstErr, &retryable) || !retryable.Retryable() {
		t.Fatalf("first-cutover error = %v, want retryable lock-busy error", firstErr)
	}
	var classified interface{ FailureClass() string }
	if !errors.As(firstErr, &classified) ||
		classified.FailureClass() != "container_image_identity_cutover_lock_busy" {
		t.Fatalf(
			"first-cutover failure class = %v, want container_image_identity_cutover_lock_busy",
			firstErr,
		)
	}
	select {
	case err := <-oldWriterDone:
		if err != nil {
			t.Fatalf("old writer after cutover rollback: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("old writer did not finish after cutover rollback")
	}

	assertContainerImageIdentityAtomicLiveCount(
		t,
		ctx,
		db,
		`SELECT count(*)
		 FROM container_image_identity_cutovers
		 WHERE scope_id = $1
		   AND generation_id = $2`,
		0,
		write.ScopeID,
		write.GenerationID,
	)
	assertContainerImageIdentityAtomicLiveCount(
		t,
		ctx,
		db,
		`SELECT count(*) FROM fact_records WHERE fact_id = ANY($1::text[])`,
		0,
		containerImageIdentityAtomicLiveFactIDs(write),
	)
	assertContainerImageIdentityAtomicLiveCount(
		t,
		ctx,
		db,
		`SELECT count(*) FROM fact_records WHERE fact_id = ANY($1::text[])`,
		len(legacyFactIDs),
		legacyFactIDs,
	)

	retryWriter := PostgresContainerImageIdentityWriter{
		DB:       db,
		Beginner: &containerImageIdentityAtomicLiveBeginner{db: db},
	}
	if _, err := retryWriter.WriteContainerImageIdentityDecisions(ctx, write); err != nil {
		t.Fatalf("retry first cutover after old writer completes: %v", err)
	}
	assertContainerImageIdentityAtomicLiveCount(
		t,
		ctx,
		db,
		`SELECT count(*) FROM fact_records WHERE fact_id = ANY($1::text[])`,
		len(write.Decisions),
		containerImageIdentityAtomicLiveFactIDs(write),
	)
	assertContainerImageIdentityAtomicLiveCount(
		t,
		ctx,
		db,
		`SELECT count(*) FROM fact_records WHERE fact_id = ANY($1::text[])`,
		0,
		legacyFactIDs,
	)
}

type containerImageIdentityPauseAfterLiveTx struct {
	tx      *sql.Tx
	pauseAt int
	calls   int
	paused  chan<- struct{}
	release <-chan struct{}
}

func (tx *containerImageIdentityPauseAfterLiveTx) ExecContext(
	ctx context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	tx.calls++
	result, err := tx.tx.ExecContext(ctx, query, args...)
	if err != nil || tx.calls != tx.pauseAt {
		return result, err
	}
	close(tx.paused)
	select {
	case <-tx.release:
		return result, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("pause after container image identity marker: %w", ctx.Err())
	}
}

func (tx *containerImageIdentityPauseAfterLiveTx) Commit() error {
	return tx.tx.Commit()
}

func (tx *containerImageIdentityPauseAfterLiveTx) Rollback() error {
	return tx.tx.Rollback()
}
