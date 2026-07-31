// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

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
				pauseAt: 4,
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
		assertContainerImageIdentityLegacyStatementRejected(t, err)
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
