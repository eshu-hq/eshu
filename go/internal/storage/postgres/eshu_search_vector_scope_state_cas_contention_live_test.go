// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"testing"
	"time"
)

// TestEshuSearchVectorScopeStateCASContentionLive is the #5045 hardening proof
// for the versioned scope-state fence CAS #4233 introduced.
//
// #4233 proved concurrency safety two ways: a SEQUENTIAL live test that feeds
// a stale fence and asserts the CAS returns false, and a fake-based runner
// tolerance test. Neither runs the CAS concurrently, so neither can observe
// what happens when N builders race for the same (scope, generation, identity)
// at once. The review dispositioned that as acceptable because the CAS is a
// single atomic statement with no FOR UPDATE and no lease relocation, so there
// is no EvalPlanQual recheck concern -- but "no recheck concern" is a claim
// about the mechanism, not a measurement of it. This test measures it.
//
// The contention shape: workersPerRound goroutines each call BeginBuilding and
// then FinalizeReady against one identity, released together (see
// runReleasedTogether) so their statements interleave in the server rather
// than running back to back.
//
// A companion test, TestEshuSearchVectorScopeStateCASContentionOverlapLive,
// covers a different shape this test deliberately does not: a retried
// BeginBuilding racing against an in-flight FinalizeReady from an earlier
// build, rather than every begin completing before any finalize starts.
//
// What it asserts, and why each matters:
//
//   - Every BeginBuilding returns a distinct fence. The fence bump is an
//     INSERT ... ON CONFLICT DO UPDATE; if two builders could observe the same
//     fence, two of them would later present an equally-current CAS token and
//     the "highest fence wins" rule would have a tie it cannot break.
//   - EXACTLY ONE FinalizeReady reports true. This is the property the whole
//     design rests on: a stale builder must not be able to publish ready over
//     a newer build.
//   - The winner is the holder of the HIGHEST fence, not merely whoever
//     committed last. A CAS that let a lower fence win would still report
//     "one winner" while silently publishing a superseded build, so counting
//     winners alone cannot catch it.
//   - The persisted row agrees with the winner. The in-process return value
//     and the durable state must not disagree.
//
// Set ESHU_SEARCH_VECTOR_SCOPE_STATE_CAS_CONTENTION_LIVE=1 and
// ESHU_POSTGRES_DSN to run. Skipped otherwise so the credential-free CI lane
// is unaffected. Override worker count with
// ESHU_SEARCH_VECTOR_SCOPE_STATE_CAS_CONTENTION_WORKERS and round count with
// ESHU_SEARCH_VECTOR_SCOPE_STATE_CAS_CONTENTION_ROUNDS; the defaults are sized
// to interleave reliably while staying quick.
func TestEshuSearchVectorScopeStateCASContentionLive(t *testing.T) {
	workers := envInt(t, "ESHU_SEARCH_VECTOR_SCOPE_STATE_CAS_CONTENTION_WORKERS", 16)
	rounds := envInt(t, "ESHU_SEARCH_VECTOR_SCOPE_STATE_CAS_CONTENTION_ROUNDS", 10)
	if workers < 2 {
		t.Fatalf("workers = %d, need at least 2 for a contention test", workers)
	}
	if rounds < 1 {
		t.Fatalf("rounds = %d, need at least 1 for a contention test", rounds)
	}

	db, sqlDB, ctx := openEshuSearchVectorContentionLiveDB(t, workers+4)

	prefix := fmt.Sprintf("5045-cas-%d", time.Now().UnixNano())
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

	for round := 0; round < rounds; round++ {
		label := fmt.Sprintf("%s:round-%d", prefix, round)
		scopeID, genID := seedEshuSearchVectorContentionFixture(t, ctx, sqlDB, label, projectionRevision, now)
		seededScopeIDs = append(seededScopeIDs, scopeID)

		type outcome struct {
			fence int64
			won   bool
			err   error
		}
		outcomes := make([]outcome, workers)

		// Two phases with a barrier between them, deliberately.
		//
		// BeginBuilding's ON CONFLICT is filtered by
		//   EXCLUDED.projection_revision > existing.projection_revision
		//   OR (equal revision AND existing.state <> 'ready')
		// so at one revision, once any builder publishes ready, a later
		// BeginBuilding matches no row and returns "no row returned". That is
		// correct supersession, not a defect -- but interleaving begins with
		// finalizes would make the run nondeterministic and would test that
		// guard rather than the CAS. Racing all the begins first, then all the
		// finalizes, puts every builder in the contended state the CAS exists
		// to arbitrate. (The overlap between a begin and an in-flight finalize
		// is exercised separately, deliberately, by the companion overlap test.)
		runReleasedTogether(workers, func(idx int) {
			fence, err := store.BeginBuilding(ctx, scopeID, genID, identity, projectionRevision)
			if err != nil {
				outcomes[idx].err = fmt.Errorf("begin building: %w", err)
				return
			}
			outcomes[idx].fence = fence
		})

		for idx, o := range outcomes {
			if o.err != nil {
				t.Fatalf("round %d worker %d: %v", round, idx, o.err)
			}
		}

		runReleasedTogether(workers, func(idx int) {
			won, err := store.FinalizeReady(ctx, scopeID, genID, identity, projectionRevision, outcomes[idx].fence)
			if err != nil {
				outcomes[idx].err = fmt.Errorf("finalize ready: %w", err)
				return
			}
			outcomes[idx].won = won
		})

		winners := 0
		winningFence := int64(-1)
		maxFence := int64(-1)
		seenFences := make(map[int64]int, workers)
		for idx, o := range outcomes {
			if o.err != nil {
				t.Fatalf("round %d worker %d: %v", round, idx, o.err)
			}
			seenFences[o.fence]++
			if o.fence > maxFence {
				maxFence = o.fence
			}
			if o.won {
				winners++
				winningFence = o.fence
			}
		}

		for fence, count := range seenFences {
			if count > 1 {
				t.Fatalf(
					"round %d: fence %d handed to %d builders, want each builder a distinct fence: "+
						"two builders holding one fence makes 'highest fence wins' undecidable",
					round, fence, count,
				)
			}
		}

		if winners != 1 {
			t.Fatalf(
				"round %d: %d of %d builders published ready, want exactly 1: "+
					"more than one means a stale build can overwrite a newer one, zero means no build publishes at all",
				round, winners, workers,
			)
		}

		if winningFence != maxFence {
			t.Fatalf(
				"round %d: fence %d won but the highest issued fence was %d: "+
					"a superseded build published over a newer one",
				round, winningFence, maxFence,
			)
		}

		// The durable row must agree with the in-process winner. A CAS that
		// returns true while persisting something else is the failure a
		// return-value-only assertion cannot see.
		var (
			persistedFence int64
			persistedState string
		)
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
			t.Fatalf("round %d: read persisted scope state: %v", round, err)
		}
		if persistedFence != winningFence {
			t.Fatalf(
				"round %d: persisted build_fence = %d, winner reported fence %d",
				round, persistedFence, winningFence,
			)
		}
		if persistedState != "ready" {
			t.Fatalf("round %d: persisted state = %q, want \"ready\"", round, persistedState)
		}
	}

	t.Logf("%d rounds x %d concurrent builders: exactly one ready publish per round, always the highest fence", rounds, workers)
}
