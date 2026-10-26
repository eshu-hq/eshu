// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8950 single-row rollout proof: a lineage-only rewrite through the single-row ContentStore keeps indexed_at and stays HOT, with the guarded upsert as the fall-through; enrolled in the live-postgres-readiness runner; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE ESHU_GENERATION_RETENTION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"fmt"
	"testing"
	"time"
)

// TestContentStoreSingleRowMetadataOnlyPreservesIndexedAtLive is the #8950
// single-row rollout regression: an unchanged-content rewrite under a new
// generation through ContentStore.UpsertFileBatch moves the lineage version,
// keeps indexed_at, and stays HOT, exactly like the batched writer.
func TestContentStoreSingleRowMetadataOnlyPreservesIndexedAtLive(t *testing.T) {
	ctx, database := openContentUpsertGuardDatabase(t)
	const repoID = "repo-8950-store"
	store := NewContentStore(SQLDB{DB: database})
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := t0
	store.Now = func() time.Time { return clock }

	bodies := map[string]string{}
	for i := 0; i < 6; i++ {
		path := fmt.Sprintf("src/f%02d.go", i)
		bodies[path] = guardedSecretBody(path, 24)
	}
	if err := store.UpsertFileBatch(ctx, repoID, "gen-1", guardedRecords("sha-1", bodies)); err != nil {
		t.Fatalf("seed UpsertFileBatch: %v", err)
	}
	prepareHotLiveTable(t, ctx, database)
	before := readContentFileVersions(t, ctx, database, repoID)

	var schema string
	if err := database.QueryRowContext(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatalf("current schema: %v", err)
	}
	if _, err := database.ExecContext(ctx, `SELECT pg_stat_force_next_flush()`); err != nil {
		t.Fatalf("force stats flush: %v", err)
	}
	var beforeUpd, beforeHot int64
	if err := database.QueryRowContext(ctx, `
SELECT n_tup_upd, n_tup_hot_upd FROM pg_stat_user_tables
WHERE schemaname = $1 AND relname = 'content_files'`, schema).Scan(&beforeUpd, &beforeHot); err != nil {
		t.Fatalf("baseline stats: %v", err)
	}

	// The clock advances so an indexed_at move would be visible; the
	// metadata-only UPDATE must not stamp it.
	clock = t0.Add(time.Hour)
	if err := store.UpsertFileBatch(ctx, repoID, "gen-2", guardedRecords("sha-2", bodies)); err != nil {
		t.Fatalf("rewrite UpsertFileBatch: %v", err)
	}

	upd, hot := hotLiveStatDeltas(t, ctx, database, schema, beforeUpd, beforeHot)
	t.Logf("single-row lineage-only rewrite: upd=%d hot=%d, stats %s", upd, hot, hotStatLine(ctx, database))
	if upd != int64(len(bodies)) || hot != int64(len(bodies)) {
		t.Errorf("n_tup_upd/hot delta = %d/%d, want %d/%d: every row must rewrite in place",
			upd, hot, len(bodies), len(bodies))
	}
	after := readContentFileVersions(t, ctx, database, repoID)
	for path, was := range before {
		now := after[path]
		if now.xmin == was.xmin {
			t.Errorf("%s kept xmin %s: the rewrite must move the lineage version", path, was.xmin)
		}
		if ctidPage(now.ctid) != ctidPage(was.ctid) {
			t.Errorf("%s left page %s for %s: not a HOT update", path, ctidPage(was.ctid), ctidPage(now.ctid))
		}
		if !now.indexedAt.Equal(was.indexedAt) {
			t.Errorf("%s moved indexed_at %s -> %s: a metadata-only rewrite must not touch it", path, was.indexedAt, now.indexedAt)
		}
		if now.commitSHA.String != "sha-2" || now.generation.String != "gen-2" {
			t.Errorf("%s lineage = (%q/%q), want (sha-2/gen-2)", path, now.commitSHA.String, now.generation.String)
		}
	}
}
