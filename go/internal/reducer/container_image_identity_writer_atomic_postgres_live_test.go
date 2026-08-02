// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"
)

// TestPostgresContainerImageIdentityCompletedCutoverAdmissionIgnoresClaimRejectedWatermark
// is the regression for a pre-push review finding (#5874 P1): the completed-
// cutover fast path's admission CTE used to be unconditional on current_claim,
// so a pass whose claim epoch had already been superseded could still win the
// admission CAS if its token happened to be issued later than the legitimate
// claimant's -- reachable under redelivery, where an old worker's nextval()
// call can land, in wall-clock terms, after the reclaiming worker's, even
// though the reclaim itself happened first. That would advance the watermark
// and wrongly reject the legitimate claimant's own retry. The fix gates the
// admission INSERT's SELECT on current_claim succeeding, so a claim-rejected
// pass advances nothing.
//
// This test proves the fixed direction end to end: a "zombie" pass with a
// STALE claim epoch but a numerically HIGHER token than any subsequent
// legitimate write is rejected on its claim (not admission), and the
// legitimate claimant's own later write -- carrying a LOWER token than the
// zombie's -- still succeeds, because the zombie never touched the
// watermark.
func TestPostgresContainerImageIdentityCompletedCutoverAdmissionIgnoresClaimRejectedWatermark(
	t *testing.T,
) {
	db := openContainerImageIdentityLivePostgres(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	seedContainerImageIdentityLiveParents(t, ctx, db)

	anchor := containerImageIdentityAtomicLiveWrite(
		"admission-ignores-rejected-anchor",
		1,
		time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC),
	)
	// zombie carries a token FAR higher than anchor's or legit's -- if the
	// pre-fix bug were still present, this token would win the admission CAS
	// despite the claim rejection, and legit's lower token would then be
	// wrongly bounced as superseded.
	zombie := containerImageIdentityAtomicLiveWrite(
		"admission-ignores-rejected-zombie",
		1,
		time.Date(2026, time.August, 1, 13, 0, 0, 0, time.UTC),
	)
	legit := containerImageIdentityAtomicLiveWrite(
		"admission-ignores-rejected-legit",
		1,
		time.Date(2026, time.August, 1, 12, 30, 0, 0, time.UTC),
	)
	cleanupContainerImageIdentityAtomicLiveWrite(t, db, anchor)
	t.Cleanup(func() {
		cleanupContainerImageIdentityAtomicLiveWrite(t, db, anchor)
		cleanupContainerImageIdentityAtomicLiveWrite(t, db, zombie)
		cleanupContainerImageIdentityAtomicLiveWrite(t, db, legit)
	})

	anchorWriter := PostgresContainerImageIdentityWriter{
		DB:       db,
		Beginner: &containerImageIdentityAtomicLiveBeginner{db: db},
	}
	if _, err := anchorWriter.WriteContainerImageIdentityDecisions(ctx, anchor); err != nil {
		t.Fatalf("complete image-reference-keyed cutover: %v", err)
	}

	// Reclaim: advance the claim epoch as a fresh claimant would, stranding
	// the (not-yet-attempted) zombie pass at the old epoch.
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET container_image_identity_claim_epoch = 2
WHERE work_item_id = $1
`, zombie.IntentID); err != nil {
		t.Fatalf("advance completed-cutover claim epoch: %v", err)
	}

	staleWriter := PostgresContainerImageIdentityWriter{
		DB:            db,
		CutoverLookup: containerImageIdentityAtomicLiveCutoverLookup{db: db},
		ClaimedExecer: containerImageIdentityAtomicLiveClaimedExecer{db: db},
	}
	if _, err := staleWriter.WriteContainerImageIdentityDecisions(ctx, zombie); err == nil {
		t.Fatal("zombie completed-cutover write error = nil, want claim rejection")
	}

	// legit reuses the SAME (now-current) claim epoch 2 and a LOWER token
	// than zombie's. Before the fix, zombie's higher token would already
	// have raised the watermark and this write would be rejected as
	// superseded instead of succeeding.
	legit.ClaimEpoch = 2
	legitWriter := PostgresContainerImageIdentityWriter{
		DB:            db,
		CutoverLookup: containerImageIdentityAtomicLiveCutoverLookup{db: db},
		ClaimedExecer: containerImageIdentityAtomicLiveClaimedExecer{db: db},
	}
	if _, err := legitWriter.WriteContainerImageIdentityDecisions(ctx, legit); err != nil {
		t.Fatalf(
			"legit completed-cutover write error = %v, want nil: the rejected zombie pass "+
				"must not have advanced the admission watermark",
			err,
		)
	}
	assertContainerImageIdentityAtomicLiveCount(
		t,
		ctx,
		db,
		`SELECT count(*) FROM fact_records WHERE fact_id = $1`,
		1,
		containerImageIdentityFactID(legit, legit.Decisions[0]),
	)
}

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
				// #5874: the admission CAS runs via tx.ExecContext (call 1)
				// before lockContainerImageIdentityCompletedCutoverClaim
				// (which uses the separate ExecContainerImageIdentityClaimed
				// method and so does not advance tx.calls). The first plain
				// ExecContext call after that claim-lock is now the first
				// batch-insert chunk, at tx.calls == 2, which is where this
				// test needs to pause so the claim-lock's own row-level
				// effect on fact_work_items is already held (uncommitted)
				// when the concurrent heartbeat races it.
				return &containerImageIdentityPausingLiveTx{
					tx: tx, pauseAt: 2, paused: paused, release: release,
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
		IntentID:     containerImageIdentityLiveWorkItemID(containerImageIdentityLiveGeneration),
		ClaimEpoch:   1,
		ScopeID:      containerImageIdentityLiveScope,
		GenerationID: containerImageIdentityLiveGeneration,
		SourceSystem: "git",
		Cause:        "synthetic atomic live proof",
		EvidenceAsOf: evidenceAsOf,
		// FencingToken (#5874) stands in for a real sequence-issued value in
		// these fixtures. Reusing evidenceAsOf's already-distinct, carefully
		// ordered instant per call site preserves each test's intended
		// relative ordering between its own writes without each call site
		// needing its own real issuer wiring; no test in this file asserts
		// evidence-freshness-vs-token ordering itself (that property is
		// covered hermetically by TestContainerImageIdentityWriterAdmissionConvergence).
		FencingToken:  evidenceAsOf.UTC().UnixMicro(),
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
