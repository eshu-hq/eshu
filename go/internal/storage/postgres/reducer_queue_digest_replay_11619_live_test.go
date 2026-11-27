// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// Test11619DigestFencedReplayQuietsUnchangedRerun proves the
// deployment-evidence replay fence end to end on live Postgres: a rerun over
// an unchanged resolved set leaves a finished workload item alone (same
// digest stays quiet), while a rerun over a changed set reopens it. The
// first pass after the fence lands still replays once by fail-open because
// the finished item carries no recorded digest yet.
func Test11619DigestFencedReplayQuietsUnchangedRerun(t *testing.T) {
	db, ctx := refinalizeRebuildResetLiveDB(t)
	suffix := testSuffix(t)
	scopeID, generationID, _ := refinalizeResetScope(t, ctx, db, suffix)
	entityKey := "repo:digest-fence-" + suffix

	queue := NewReducerQueue(SQLDB{DB: db}, "digest-fence-worker-"+suffix, time.Minute)
	queue.ClaimDomain = reducer.DomainWorkloadMaterialization

	const firstDigest = "digest-first-11619"
	const secondDigest = "digest-second-11619"

	// No row yet: the replay schedules fresh work.
	if replayed, err := queue.ReplayWorkloadMaterializationForDigest(ctx, scopeID, generationID, entityKey, firstDigest); err != nil {
		t.Fatalf("schedule first digest replay: %v", err)
	} else if !replayed {
		t.Fatal("schedule first digest replay = false, want true for missing work")
	}
	succeedWorkloadItem(t, ctx, db, scopeID, generationID)

	// Finished with no recorded digest: fail-open replays once and records it.
	if replayed, err := queue.ReplayWorkloadMaterializationForDigest(ctx, scopeID, generationID, entityKey, firstDigest); err != nil {
		t.Fatalf("schedule unrecorded-digest replay: %v", err)
	} else if !replayed {
		t.Fatal("schedule unrecorded-digest replay = false, want fail-open true")
	}
	assertWorkloadDigestState(t, ctx, db, scopeID, generationID, "pending", firstDigest)
	succeedWorkloadItem(t, ctx, db, scopeID, generationID)

	before := readWorkloadDigestState(t, ctx, db, scopeID, generationID)

	// Unchanged resolved set: the rerun stays quiet.
	if replayed, err := queue.ReplayWorkloadMaterializationForDigest(ctx, scopeID, generationID, entityKey, firstDigest); err != nil {
		t.Fatalf("schedule unchanged-digest replay: %v", err)
	} else if replayed {
		t.Fatal("schedule unchanged-digest replay = true, want quiet false")
	}
	after := readWorkloadDigestState(t, ctx, db, scopeID, generationID)
	if after.status != "succeeded" {
		t.Fatalf("quiet rerun status = %q, want succeeded", after.status)
	}
	if after.digest != firstDigest {
		t.Fatalf("quiet rerun digest = %q, want %q", after.digest, firstDigest)
	}
	if !after.updatedAt.Equal(before.updatedAt) {
		t.Fatalf("quiet rerun moved updated_at from %v to %v", before.updatedAt, after.updatedAt)
	}
	if !after.reopenedAt.Equal(before.reopenedAt) {
		t.Fatalf("quiet rerun moved reopened_at from %v to %v", before.reopenedAt, after.reopenedAt)
	}

	// Changed resolved set: the rerun reopens.
	if replayed, err := queue.ReplayWorkloadMaterializationForDigest(ctx, scopeID, generationID, entityKey, secondDigest); err != nil {
		t.Fatalf("schedule changed-digest replay: %v", err)
	} else if !replayed {
		t.Fatal("schedule changed-digest replay = false, want true")
	}
	assertWorkloadDigestState(t, ctx, db, scopeID, generationID, "pending", secondDigest)
}

// Test11619DigestFencedReplayConcurrentRerunsStayQuiet proves concurrent
// reruns over an unchanged resolved set are idempotent: every caller reports
// quiet and the finished row is untouched, with no new lock beyond the same
// single-row update the replay already takes.
func Test11619DigestFencedReplayConcurrentRerunsStayQuiet(t *testing.T) {
	db, ctx := refinalizeRebuildResetLiveDB(t)
	suffix := testSuffix(t)
	scopeID, generationID, _ := refinalizeResetScope(t, ctx, db, suffix)
	entityKey := "repo:digest-fence-race-" + suffix

	queue := NewReducerQueue(SQLDB{DB: db}, "digest-fence-race-worker-"+suffix, time.Minute)
	queue.ClaimDomain = reducer.DomainWorkloadMaterialization

	const digest = "digest-race-11619"
	if _, err := queue.ReplayWorkloadMaterializationForDigest(ctx, scopeID, generationID, entityKey, digest); err != nil {
		t.Fatalf("seed digest replay: %v", err)
	}
	if _, err := queue.ReplayWorkloadMaterializationForDigest(ctx, scopeID, generationID, entityKey, digest); err != nil {
		t.Fatalf("record digest replay: %v", err)
	}
	succeedWorkloadItem(t, ctx, db, scopeID, generationID)
	before := readWorkloadDigestState(t, ctx, db, scopeID, generationID)
	if before.status != "succeeded" || before.digest != digest {
		t.Fatalf("seeded state = %+v, want succeeded with digest %q", before, digest)
	}

	const callers = 8
	var wg sync.WaitGroup
	results := make([]bool, callers)
	errs := make([]error, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = queue.ReplayWorkloadMaterializationForDigest(ctx, scopeID, generationID, entityKey, digest)
		}(i)
	}
	wg.Wait()
	for i := 0; i < callers; i++ {
		if errs[i] != nil {
			t.Fatalf("concurrent replay %d: %v", i, errs[i])
		}
		if results[i] {
			t.Fatalf("concurrent replay %d = true, want quiet false", i)
		}
	}
	after := readWorkloadDigestState(t, ctx, db, scopeID, generationID)
	if after.status != "succeeded" || after.digest != digest {
		t.Fatalf("concurrent reruns left %+v, want %+v", after, before)
	}
	if !after.updatedAt.Equal(before.updatedAt) || !after.reopenedAt.Equal(before.reopenedAt) {
		t.Fatalf("concurrent reruns moved timestamps from %+v to %+v", before, after)
	}
}

type workloadDigestState struct {
	status     string
	digest     string
	updatedAt  time.Time
	reopenedAt time.Time
}

func succeedWorkloadItem(t *testing.T, ctx context.Context, db *sql.DB, scopeID, generationID string) {
	t.Helper()
	if _, err := db.ExecContext(ctx,
		`UPDATE fact_work_items SET status = 'succeeded', lease_owner = NULL, claim_until = NULL
		 WHERE scope_id = $1 AND generation_id = $2 AND domain = $3 AND stage = 'reducer'`,
		scopeID, generationID, string(reducer.DomainWorkloadMaterialization),
	); err != nil {
		t.Fatalf("mark workload item succeeded: %v", err)
	}
}

func readWorkloadDigestState(t *testing.T, ctx context.Context, db *sql.DB, scopeID, generationID string) workloadDigestState {
	t.Helper()
	var state workloadDigestState
	if err := db.QueryRowContext(ctx,
		`SELECT status, COALESCE(payload->>'deployment_evidence_digest', ''), updated_at, COALESCE(reopened_at, updated_at)
		 FROM fact_work_items
		 WHERE scope_id = $1 AND generation_id = $2 AND domain = $3 AND stage = 'reducer'`,
		scopeID, generationID, string(reducer.DomainWorkloadMaterialization),
	).Scan(&state.status, &state.digest, &state.updatedAt, &state.reopenedAt); err != nil {
		t.Fatalf("read workload digest state: %v", err)
	}
	return state
}

func assertWorkloadDigestState(t *testing.T, ctx context.Context, db *sql.DB, scopeID, generationID, wantStatus, wantDigest string) {
	t.Helper()
	state := readWorkloadDigestState(t, ctx, db, scopeID, generationID)
	if state.status != wantStatus {
		t.Fatalf("workload status = %q, want %q", state.status, wantStatus)
	}
	if state.digest != wantDigest {
		t.Fatalf("workload digest = %q, want %q", state.digest, wantDigest)
	}
}
