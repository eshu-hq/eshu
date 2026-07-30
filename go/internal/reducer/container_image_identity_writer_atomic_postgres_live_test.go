// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
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
	if err := reducerBatchInsertFacts(
		ctx,
		db,
		[]reducerFactRow{containerImageIdentityLegacyLiveRow(legacyFactID, 0, false)},
	); err != nil {
		t.Fatalf("run old-binary legacy writer after completed cutover: %v", err)
	}
	assertContainerImageIdentityAtomicLiveCount(
		t,
		ctx,
		db,
		`SELECT count(*) FROM fact_records WHERE fact_id = $1`,
		0,
		legacyFactID,
	)
}

func TestPostgresContainerImageIdentityMultiChunkWriterSerializesMatchingKeyAndCleansInterleavedLegacyWrite(
	t *testing.T,
) {
	db := openContainerImageIdentityLivePostgres(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	seedContainerImageIdentityLiveParents(t, ctx, db)

	write := containerImageIdentityAtomicLiveWrite(
		"rolling-race",
		reducerFactBatchSize+1,
		time.Date(2026, time.July, 29, 16, 0, 0, 0, time.UTC),
	)
	cleanupContainerImageIdentityAtomicLiveWrite(t, db, write)
	t.Cleanup(func() {
		cleanupContainerImageIdentityAtomicLiveWrite(t, db, write)
	})

	paused := make(chan struct{})
	release := make(chan struct{})
	beginner := &containerImageIdentityAtomicLiveBeginner{
		db: db,
		wrap: func(tx *sql.Tx) ContainerImageIdentityTransaction {
			return &containerImageIdentityPausingLiveTx{
				tx:      tx,
				pauseAt: 3,
				paused:  paused,
				release: release,
			}
		},
	}
	writer := PostgresContainerImageIdentityWriter{
		DB:       db,
		Beginner: beginner,
	}
	writerDone := make(chan error, 1)
	go func() {
		_, err := writer.WriteContainerImageIdentityDecisions(ctx, write)
		writerDone <- err
	}()

	select {
	case <-paused:
	case <-ctx.Done():
		t.Fatal("multi-chunk writer did not pause before its final publication and cleanup")
	}

	firstDecision := write.Decisions[0]
	firstFactID := containerImageIdentityFactID(write, firstDecision)
	freshToken := write.EvidenceAsOf.Add(time.Second).UnixMicro()
	conflictingDone := make(chan error, 1)
	go func() {
		conflictingDone <- reducerBatchInsertFacts(
			ctx,
			db,
			[]reducerFactRow{containerImageIdentityLiveRow(firstFactID, freshToken, false)},
		)
	}()
	select {
	case err := <-conflictingDone:
		t.Fatalf("matching-key upsert returned before the multi-chunk transaction committed: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	legacyFactID := write.LegacyFactIDs[len(write.LegacyFactIDs)-1]
	legacyDone := make(chan error, 1)
	go func() {
		legacyDone <- reducerBatchInsertFacts(
			ctx,
			db,
			[]reducerFactRow{containerImageIdentityLegacyLiveRow(legacyFactID, 0, false)},
		)
	}()
	select {
	case err := <-legacyDone:
		t.Fatalf("legacy insert returned before the cutover transaction committed: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	select {
	case err := <-writerDone:
		if err != nil {
			t.Fatalf("multi-chunk writer after interleaved legacy write: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("multi-chunk writer did not finish after release")
	}
	select {
	case err := <-conflictingDone:
		if err != nil {
			t.Fatalf("matching-key upsert after transaction release: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("matching-key upsert did not resume after transaction release")
	}
	select {
	case err := <-legacyDone:
		if err != nil {
			t.Fatalf("legacy insert after cutover release: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("legacy insert did not resume after cutover release")
	}

	assertContainerImageIdentityLiveRow(t, ctx, db, firstFactID, false, freshToken)
	assertContainerImageIdentityAtomicLiveCount(
		t,
		ctx,
		db,
		`SELECT count(*) FROM fact_records WHERE fact_id = $1`,
		0,
		legacyFactID,
	)
	assertContainerImageIdentityAtomicLiveCount(
		t,
		ctx,
		db,
		`SELECT count(*) FROM fact_records WHERE fact_id = ANY($1::text[]) AND is_tombstone = FALSE`,
		len(write.Decisions),
		containerImageIdentityAtomicLiveFactIDs(write),
	)
}

func TestPostgresContainerImageIdentityMultiChunkFailureRollsBackAndRetryConverges(
	t *testing.T,
) {
	db := openContainerImageIdentityLivePostgres(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	seedContainerImageIdentityLiveParents(t, ctx, db)

	write := containerImageIdentityAtomicLiveWrite(
		"rollback-retry",
		5*reducerFactBatchSize+1,
		time.Date(2026, time.July, 29, 16, 1, 0, 0, time.UTC),
	)
	cleanupContainerImageIdentityAtomicLiveWrite(t, db, write)
	t.Cleanup(func() {
		cleanupContainerImageIdentityAtomicLiveWrite(t, db, write)
	})

	legacyFactID := write.LegacyFactIDs[0]
	if err := reducerBatchInsertFacts(
		ctx,
		db,
		[]reducerFactRow{containerImageIdentityLegacyLiveRow(legacyFactID, 0, false)},
	); err != nil {
		t.Fatalf("seed old-binary legacy row: %v", err)
	}

	failingWriter := PostgresContainerImageIdentityWriter{
		DB: db,
		Beginner: &containerImageIdentityAtomicLiveBeginner{
			db: db,
			wrap: func(tx *sql.Tx) ContainerImageIdentityTransaction {
				return &containerImageIdentityFailingLiveTx{
					tx:     tx,
					failAt: 3,
				}
			},
		},
	}
	_, err := failingWriter.WriteContainerImageIdentityDecisions(ctx, write)
	if err == nil || !strings.Contains(err.Error(), "batch insert reducer facts") {
		t.Fatalf("mid-transaction write error = %v, want injected chunk failure", err)
	}
	assertContainerImageIdentityAtomicLiveCount(
		t,
		ctx,
		db,
		`SELECT count(*) FROM fact_records WHERE source_fact_key = $1`,
		0,
		write.IntentID,
	)
	assertContainerImageIdentityAtomicLiveCount(
		t,
		ctx,
		db,
		`SELECT count(*) FROM fact_records WHERE fact_id = $1`,
		1,
		legacyFactID,
	)

	retryWriter := PostgresContainerImageIdentityWriter{
		DB:       db,
		Beginner: &containerImageIdentityAtomicLiveBeginner{db: db},
	}
	if _, err := retryWriter.WriteContainerImageIdentityDecisions(ctx, write); err != nil {
		t.Fatalf("retry multi-chunk write after rollback: %v", err)
	}
	assertContainerImageIdentityAtomicLiveCount(
		t,
		ctx,
		db,
		`SELECT count(*) FROM fact_records WHERE fact_id = ANY($1::text[]) AND is_tombstone = FALSE`,
		len(write.Decisions),
		containerImageIdentityAtomicLiveFactIDs(write),
	)
	assertContainerImageIdentityAtomicLiveCount(
		t,
		ctx,
		db,
		`SELECT count(*) FROM fact_records WHERE fact_id = $1`,
		0,
		legacyFactID,
	)
}

func containerImageIdentityAtomicLiveWrite(
	prefix string,
	decisionCount int,
	evidenceAsOf time.Time,
) ContainerImageIdentityWrite {
	write := ContainerImageIdentityWrite{
		IntentID:      "intent-5854-" + prefix,
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
