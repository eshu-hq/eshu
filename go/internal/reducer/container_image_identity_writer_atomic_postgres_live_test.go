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

func TestPostgresContainerImageIdentityCompletedCutoverRejectsLaterLegacyWriter(
	t *testing.T,
) {
	db := openContainerImageIdentityLivePostgres(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	seedContainerImageIdentityLiveParents(t, ctx, db)

	write := containerImageIdentityAtomicLiveWrite(
		"legacy-after-cutover",
		1,
		time.Date(2026, time.July, 29, 15, 59, 0, 0, time.UTC),
	)
	cleanupContainerImageIdentityAtomicLiveWrite(t, db, write)
	t.Cleanup(func() {
		cleanupContainerImageIdentityAtomicLiveWrite(t, db, write)
	})

	writer := PostgresContainerImageIdentityWriter{
		DB:       db,
		Beginner: &containerImageIdentityAtomicLiveBeginner{db: db},
	}
	if _, err := writer.WriteContainerImageIdentityDecisions(ctx, write); err != nil {
		t.Fatalf("complete image-reference-keyed cutover: %v", err)
	}

	legacyFactID := write.LegacyFactIDs[0]
	err := reducerBatchInsertFacts(
		ctx,
		db,
		[]reducerFactRow{containerImageIdentityLegacyLiveRow(legacyFactID, 0, false)},
	)
	assertContainerImageIdentityLegacyStatementRejected(t, err)
	assertContainerImageIdentityAtomicLiveCount(
		t,
		ctx,
		db,
		`SELECT count(*) FROM fact_records WHERE fact_id = $1`,
		0,
		legacyFactID,
	)
}

func TestPostgresContainerImageIdentityCompletedCutoverRejectsStaleClaimEpoch(
	t *testing.T,
) {
	db := openContainerImageIdentityLivePostgres(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	seedContainerImageIdentityLiveParents(t, ctx, db)

	anchor := containerImageIdentityAtomicLiveWrite(
		"completed-cutover-anchor",
		1,
		time.Date(2026, time.July, 29, 15, 59, 0, 0, time.UTC),
	)
	stale := containerImageIdentityAtomicLiveWrite(
		"completed-cutover-stale",
		1,
		time.Date(2026, time.July, 29, 16, 0, 0, 0, time.UTC),
	)
	cleanupContainerImageIdentityAtomicLiveWrite(t, db, anchor)
	t.Cleanup(func() {
		cleanupContainerImageIdentityAtomicLiveWrite(t, db, anchor)
		cleanupContainerImageIdentityAtomicLiveWrite(t, db, stale)
	})

	anchorWriter := PostgresContainerImageIdentityWriter{
		DB:       db,
		Beginner: &containerImageIdentityAtomicLiveBeginner{db: db},
	}
	if _, err := anchorWriter.WriteContainerImageIdentityDecisions(ctx, anchor); err != nil {
		t.Fatalf("complete image-reference-keyed cutover: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET container_image_identity_claim_epoch = 2
WHERE work_item_id = $1
`, stale.IntentID); err != nil {
		t.Fatalf("advance completed-cutover claim epoch: %v", err)
	}

	staleWriter := PostgresContainerImageIdentityWriter{
		DB:            db,
		Beginner:      &containerImageIdentityAtomicLiveBeginner{db: db},
		CutoverLookup: containerImageIdentityAtomicLiveCutoverLookup{db: db},
		ClaimedExecer: containerImageIdentityAtomicLiveClaimedExecer{db: db},
	}
	if _, err := staleWriter.WriteContainerImageIdentityDecisions(ctx, stale); err == nil {
		t.Fatal("stale completed-cutover write error = nil, want claim rejection")
	}
	assertContainerImageIdentityAtomicLiveCount(
		t,
		ctx,
		db,
		`SELECT count(*) FROM fact_records WHERE fact_id = $1`,
		0,
		containerImageIdentityFactID(stale, stale.Decisions[0]),
	)
}

func TestPostgresContainerImageIdentityCompletedCutoverMultiChunkKeepsHeartbeatLive(
	t *testing.T,
) {
	db := openContainerImageIdentityLivePostgres(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	seedContainerImageIdentityLiveParents(t, ctx, db)

	anchor := containerImageIdentityAtomicLiveWrite(
		"completed-heartbeat-anchor",
		1,
		time.Date(2026, time.July, 29, 16, 0, 0, 0, time.UTC),
	)
	write := containerImageIdentityAtomicLiveWrite(
		"completed-heartbeat-multi",
		reducerFactBatchSize+1,
		time.Date(2026, time.July, 29, 16, 1, 0, 0, time.UTC),
	)
	cleanupContainerImageIdentityAtomicLiveWrite(t, db, anchor)
	t.Cleanup(func() {
		cleanupContainerImageIdentityAtomicLiveWrite(t, db, anchor)
		cleanupContainerImageIdentityAtomicLiveWrite(t, db, write)
	})
	anchorWriter := PostgresContainerImageIdentityWriter{
		DB:       db,
		Beginner: &containerImageIdentityAtomicLiveBeginner{db: db},
	}
	if _, err := anchorWriter.WriteContainerImageIdentityDecisions(ctx, anchor); err != nil {
		t.Fatalf("complete heartbeat-proof cutover: %v", err)
	}

	paused := make(chan struct{})
	release := make(chan struct{})
	writer := PostgresContainerImageIdentityWriter{
		DB:            db,
		CutoverLookup: containerImageIdentityAtomicLiveCutoverLookup{db: db},
		ClaimedExecer: containerImageIdentityAtomicLiveClaimedExecer{db: db},
		Beginner: &containerImageIdentityAtomicLiveBeginner{
			db: db,
			wrap: func(tx *sql.Tx) ContainerImageIdentityTransaction {
				return &containerImageIdentityPausingLiveTx{
					tx: tx, pauseAt: 1, paused: paused, release: release,
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
	case <-paused:
	case <-ctx.Done():
		t.Fatal("completed-cutover writer did not pause after claim lock")
	}

	heartbeatDone := make(chan error, 1)
	go func() {
		result, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET claim_until = clock_timestamp() + interval '5 minutes',
    updated_at = clock_timestamp()
WHERE work_item_id = $1
  AND stage = 'reducer'
  AND lease_owner = 'reducer'
  AND status IN ('claimed', 'running')
`, write.IntentID)
		if err == nil {
			var affected int64
			affected, err = result.RowsAffected()
			if err == nil && affected != 1 {
				err = fmt.Errorf("heartbeat rows affected = %d, want 1", affected)
			}
		}
		heartbeatDone <- err
	}()
	select {
	case err := <-heartbeatDone:
		t.Fatalf("heartbeat returned before completed-cutover commit: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	select {
	case err := <-writerDone:
		if err != nil {
			t.Fatalf("completed-cutover multi-chunk writer: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("completed-cutover multi-chunk writer did not finish")
	}
	select {
	case err := <-heartbeatDone:
		if err != nil {
			t.Fatalf("heartbeat after completed-cutover commit: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("heartbeat did not resume after completed-cutover commit")
	}
}

func containerImageIdentityAtomicLiveWrite(
	prefix string,
	decisionCount int,
	evidenceAsOf time.Time,
) ContainerImageIdentityWrite {
	write := ContainerImageIdentityWrite{
		IntentID:      containerImageIdentityLiveWorkItemID(containerImageIdentityLiveGeneration),
		ClaimEpoch:    1,
		ScopeID:       containerImageIdentityLiveScope,
		GenerationID:  containerImageIdentityLiveGeneration,
		SourceSystem:  "git",
		Cause:         "synthetic atomic live proof",
		EvidenceAsOf:  evidenceAsOf,
		Decisions:     make([]ContainerImageIdentityDecision, 0, decisionCount),
		LegacyFactIDs: make([]string, 0, decisionCount),
	}
	for index := range decisionCount {
		decision := ContainerImageIdentityDecision{
			ImageRef: fmt.Sprintf(
				"registry.example.com/team/api:%s-%05d",
				prefix,
				index,
			),
			Digest:          retirementTestDigest,
			RepositoryID:    retirementTestRepositoryID,
			Outcome:         ContainerImageIdentityTagResolved,
			CanonicalWrites: 1,
		}
		write.Decisions = append(write.Decisions, decision)
		write.LegacyFactIDs = append(
			write.LegacyFactIDs,
			legacyContainerImageIdentityFactID(write, decision),
		)
	}
	return write
}

func cleanupContainerImageIdentityAtomicLiveWrite(
	t *testing.T,
	db *sql.DB,
	write ContainerImageIdentityWrite,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := db.ExecContext(
		ctx,
		`DELETE FROM fact_records
		 WHERE fact_id = ANY($1::text[])
		    OR fact_id = ANY($2::text[])`,
		containerImageIdentityAtomicLiveFactIDs(write),
		write.LegacyFactIDs,
	); err != nil {
		t.Errorf("clean atomic live facts for %q: %v", write.IntentID, err)
	}
	if _, err := db.ExecContext(
		ctx,
		`DELETE FROM container_image_identity_cutovers
		 WHERE scope_id = $1
		   AND generation_id = $2`,
		write.ScopeID,
		write.GenerationID,
	); err != nil {
		t.Errorf("clean atomic live cutover for %q: %v", write.IntentID, err)
	}
}

func containerImageIdentityAtomicLiveFactIDs(
	write ContainerImageIdentityWrite,
) []string {
	factIDs := make([]string, 0, len(write.Decisions))
	for _, decision := range write.Decisions {
		factIDs = append(factIDs, containerImageIdentityFactID(write, decision))
	}
	return factIDs
}

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
