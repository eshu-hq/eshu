// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestEshuSearchVectorScopeStateCASContentionOverlapLive is the PR #6039
// review follow-up for the shape TestEshuSearchVectorScopeStateCASContentionLive
// deliberately does not cover.
//
// That test's beginDone.Wait() barrier ensures every BeginBuilding commits
// before any FinalizeReady starts, so by the time a finalize phase begins,
// every fence but the highest is already stale -- the phase would pass even
// if every finalize ran one at a time with no real overlap at all. It proves
// "highest fence wins among finalizers holding fences that already exist,"
// not "a fresh BeginBuilding racing a still-in-flight FinalizeReady cannot
// reopen a build that finalize is about to publish, or already did."
//
// This test forces that overlap: N workers race FinalizeReady against their
// already-issued fences from an initial begin phase, and, released together
// with them, one more worker calls BeginBuilding again -- a retry racing the
// in-flight finalizes rather than waiting for them.
//
// Because eshu_search_vector_scope_state has one row per
// (scope, generation, identity) and both statements are single atomic
// UPDATE/INSERT ... ON CONFLICT statements against that row, Postgres's
// per-statement atomicity makes exactly one of two outcomes possible for
// each attempt, never a third:
//
//   - A finalize wins (its fence still matched the row when its UPDATE ran):
//     the retry's BeginBuilding must then find state = 'ready' at the same
//     revision and match no row ("no row returned"). If it instead reopens
//     the row into 'building', a just-published ready build was silently
//     reverted underneath its own readers.
//   - The retry wins (its BeginBuilding's fence bump commits while every
//     finalize still holds a now-stale fence): every finalize must then find
//     its fence no longer matches and report false. If any finalize wins
//     anyway, a stale build published over one that had already retried.
//
// Set ESHU_SEARCH_VECTOR_SCOPE_STATE_CAS_CONTENTION_LIVE=1 and
// ESHU_POSTGRES_DSN to run (the same gate as the sibling contention test).
// Override worker count with
// ESHU_SEARCH_VECTOR_SCOPE_STATE_CAS_CONTENTION_OVERLAP_WORKERS and attempt
// count -- repeated on fresh fixtures to raise the odds of observing both
// orderings -- with ESHU_SEARCH_VECTOR_SCOPE_STATE_CAS_CONTENTION_OVERLAP_ATTEMPTS.
func TestEshuSearchVectorScopeStateCASContentionOverlapLive(t *testing.T) {
	workers := envInt(t, "ESHU_SEARCH_VECTOR_SCOPE_STATE_CAS_CONTENTION_OVERLAP_WORKERS", 8)
	attempts := envInt(t, "ESHU_SEARCH_VECTOR_SCOPE_STATE_CAS_CONTENTION_OVERLAP_ATTEMPTS", 20)
	if workers < 2 {
		t.Fatalf("workers = %d, need at least 2 for a contention test", workers)
	}
	if attempts < 1 {
		t.Fatalf("attempts = %d, need at least 1 for a contention test", attempts)
	}

	db, sqlDB, ctx := openEshuSearchVectorContentionLiveDB(t, workers+8)

	prefix := fmt.Sprintf("6039-overlap-%d", time.Now().UnixNano())
	identity := EshuSearchVectorIdentity{
		ProviderProfileID:  "semantic-search-default",
		SourceClass:        "search_documents",
		EmbeddingModelID:   "local-hash-v1",
		VectorIndexVersion: "vector-v1",
	}
	now := time.Now().UTC()

	var seededScopeIDs []string
	registerEshuSearchVectorContentionCleanup(t, sqlDB, &seededScopeIDs)

	store := NewEshuSearchVectorScopeStateStore(db)

	const projectionRevision int64 = 1

	retryWins, finalizeWins := 0, 0
	for attempt := 0; attempt < attempts; attempt++ {
		label := fmt.Sprintf("%s:attempt-%d", prefix, attempt)
		scopeID, genID := seedEshuSearchVectorContentionFixture(t, ctx, sqlDB, label, projectionRevision, now)
		seededScopeIDs = append(seededScopeIDs, scopeID)

		// Phase 1: an ordinary begin phase establishes workers distinct
		// fences, exactly as the sibling contention test does.
		fences := make([]int64, workers)
		runReleasedTogether(workers, func(idx int) {
			fence, err := store.BeginBuilding(ctx, scopeID, genID, identity, projectionRevision)
			if err != nil {
				t.Fatalf("attempt %d worker %d: begin building: %v", attempt, idx, err)
			}
			fences[idx] = fence
		})
		maxFenceP1 := int64(-1)
		for _, f := range fences {
			if f > maxFenceP1 {
				maxFenceP1 = f
			}
		}

		// Phase 2: the overlap. `workers` goroutines finalize against their
		// phase-1 fences while, released in the same runReleasedTogether
		// batch, one more goroutine calls BeginBuilding again -- a retry
		// racing the in-flight finalizes instead of waiting for them.
		type finalizeOutcome struct {
			won bool
			err error
		}
		finalizeOutcomes := make([]finalizeOutcome, workers)
		var retryFence int64
		var retryErr error

		runReleasedTogether(workers+1, func(idx int) {
			if idx == workers {
				retryFence, retryErr = store.BeginBuilding(ctx, scopeID, genID, identity, projectionRevision)
				return
			}
			won, err := store.FinalizeReady(ctx, scopeID, genID, identity, projectionRevision, fences[idx])
			if err != nil {
				finalizeOutcomes[idx].err = fmt.Errorf("finalize ready: %w", err)
				return
			}
			finalizeOutcomes[idx].won = won
		})

		finalizeWinners := 0
		for idx, o := range finalizeOutcomes {
			if o.err != nil {
				t.Fatalf("attempt %d worker %d: %v", attempt, idx, o.err)
			}
			if o.won {
				finalizeWinners++
			}
		}
		if finalizeWinners > 1 {
			t.Fatalf(
				"attempt %d: %d finalizers reported success, want at most 1: "+
					"more than one stale fence cannot both win a single-row CAS",
				attempt, finalizeWinners,
			)
		}

		var persistedFence int64
		var persistedState string
		if err := sqlDB.QueryRowContext(
			ctx, `
			SELECT build_fence, state
			  FROM eshu_search_vector_scope_state
			 WHERE scope_id = $1 AND generation_id = $2
			   AND provider_profile_id = $3 AND source_class = $4
			   AND embedding_model_id = $5 AND vector_index_version = $6`,
			scopeID, genID,
			identity.ProviderProfileID, identity.SourceClass,
			identity.EmbeddingModelID, identity.VectorIndexVersion,
		).Scan(&persistedFence, &persistedState); err != nil {
			t.Fatalf("attempt %d: read persisted scope state: %v", attempt, err)
		}

		switch {
		case finalizeWinners == 1 && retryErr == nil:
			t.Fatalf(
				"attempt %d: a finalize published ready AND the concurrent retry begin also "+
					"succeeded (new fence %d): the retry silently reopened a build that had "+
					"already published, reverting the just-published ready state",
				attempt, retryFence,
			)
		case finalizeWinners == 1 && retryErr != nil:
			if !strings.Contains(retryErr.Error(), "no row returned") {
				t.Fatalf("attempt %d: retry begin failed for an unexpected reason: %v", attempt, retryErr)
			}
			if persistedState != "ready" || persistedFence != maxFenceP1 {
				t.Fatalf(
					"attempt %d: a finalize won but persisted state = (%q, fence %d), want (\"ready\", %d)",
					attempt, persistedState, persistedFence, maxFenceP1,
				)
			}
			finalizeWins++
		case finalizeWinners == 0 && retryErr == nil:
			if retryFence <= maxFenceP1 {
				t.Fatalf(
					"attempt %d: retry begin won with fence %d, want strictly greater than phase-1 max %d",
					attempt, retryFence, maxFenceP1,
				)
			}
			if persistedState != "building" || persistedFence != retryFence {
				t.Fatalf(
					"attempt %d: retry won but persisted state = (%q, fence %d), want (\"building\", %d): "+
						"a stale finalize must not have published over the retry's bump",
					attempt, persistedState, persistedFence, retryFence,
				)
			}
			retryWins++
		default: // finalizeWinners == 0 && retryErr != nil
			t.Fatalf(
				"attempt %d: neither a finalize nor the retry begin succeeded (retry error: %v): "+
					"exactly one of the two concurrent writers must win the row",
				attempt, retryErr,
			)
		}
	}

	t.Logf(
		"%d attempts x %d finalizers racing 1 retried begin: %d attempts the retry won, %d attempts a finalize won first",
		attempts, workers, retryWins, finalizeWins,
	)
}
