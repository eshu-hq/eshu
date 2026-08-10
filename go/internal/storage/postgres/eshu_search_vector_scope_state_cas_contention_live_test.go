// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
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
// then FinalizeReady against one identity, released together through a
// sync.WaitGroup so their statements interleave in the server rather than
// running back to back.
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
	if os.Getenv("ESHU_SEARCH_VECTOR_SCOPE_STATE_CAS_CONTENTION_LIVE") != "1" {
		t.Skip("set ESHU_SEARCH_VECTOR_SCOPE_STATE_CAS_CONTENTION_LIVE=1 and ESHU_POSTGRES_DSN to run")
	}
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN not set")
	}

	workers := envInt(t, "ESHU_SEARCH_VECTOR_SCOPE_STATE_CAS_CONTENTION_WORKERS", 16)
	rounds := envInt(t, "ESHU_SEARCH_VECTOR_SCOPE_STATE_CAS_CONTENTION_ROUNDS", 10)
	if workers < 2 {
		t.Fatalf("workers = %d, need at least 2 for a contention test", workers)
	}

	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()
	// Every worker needs its own connection or the goroutines serialize in the
	// pool and the race never actually happens in the server.
	sqlDB.SetMaxOpenConns(workers + 4)
	sqlDB.SetMaxIdleConns(workers + 4)
	db := SQLDB{DB: sqlDB}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	if err := ApplyBootstrap(ctx, db); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}

	prefix := fmt.Sprintf("5045-cas-%d", time.Now().UnixNano())
	identity := EshuSearchVectorIdentity{
		ProviderProfileID:  "semantic-search-default",
		SourceClass:        "search_documents",
		EmbeddingModelID:   "local-hash-v1",
		VectorIndexVersion: "vector-v1",
	}
	now := time.Now().UTC()

	var seededScopeIDs []string
	t.Cleanup(func() {
		cleanCtx := context.Background()
		for _, scopeID := range seededScopeIDs {
			_, _ = sqlDB.ExecContext(cleanCtx, `DELETE FROM ingestion_scopes WHERE scope_id = $1`, scopeID)
		}
	})

	store := NewEshuSearchVectorScopeStateStore(db)

	for round := 0; round < rounds; round++ {
		scopeID := fmt.Sprintf("%s:scope-%d", prefix, round)
		genID := fmt.Sprintf("%s:gen-%d", prefix, round)
		seededScopeIDs = append(seededScopeIDs, scopeID)

		if _, err := sqlDB.ExecContext(
			ctx, `
			INSERT INTO ingestion_scopes
			  (scope_id, scope_kind, source_system, source_key, collector_kind,
			   partition_key, observed_at, ingested_at, status, active_generation_id, payload)
			VALUES ($1::text, 'repository', 'git', $1::text, 'git', $1::text, $2, $2, 'active', $3::text,
			        jsonb_build_object('repo_id', $1::text))
			ON CONFLICT (scope_id) DO NOTHING`,
			scopeID, now, genID,
		); err != nil {
			t.Fatalf("round %d: insert ingestion_scope: %v", round, err)
		}
		if _, err := sqlDB.ExecContext(
			ctx, `
			INSERT INTO scope_generations
			  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
			VALUES ($1, $2, 'manual', $3, $3, 'active', $3)
			ON CONFLICT (generation_id) DO NOTHING`,
			genID, scopeID, now,
		); err != nil {
			t.Fatalf("round %d: insert scope_generation: %v", round, err)
		}

		const projectionRevision int64 = 1

		// BeginBuilding only issues a fence when the document projection for
		// this (scope, generation) is already 'ready' at the same revision --
		// it SELECTs that row as its insert source. Seed it directly so the
		// test exercises the vector-state CAS rather than the projection
		// pipeline.
		if _, err := sqlDB.ExecContext(
			ctx, `
			INSERT INTO eshu_search_document_projection_state
			  (scope_id, generation_id, projection_revision, build_fence, state, document_count, updated_at)
			VALUES ($1, $2, $3, 1, 'ready', 1, $4)
			ON CONFLICT (scope_id, generation_id) DO UPDATE SET
			  projection_revision = EXCLUDED.projection_revision,
			  state = 'ready',
			  updated_at = EXCLUDED.updated_at`,
			scopeID, genID, projectionRevision, now,
		); err != nil {
			t.Fatalf("round %d: seed projection state: %v", round, err)
		}

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
		// to arbitrate.
		var beginRelease, beginDone sync.WaitGroup
		beginRelease.Add(1)
		for w := 0; w < workers; w++ {
			beginDone.Add(1)
			go func(idx int) {
				defer beginDone.Done()
				beginRelease.Wait()
				fence, err := store.BeginBuilding(ctx, scopeID, genID, identity, projectionRevision)
				if err != nil {
					outcomes[idx] = outcome{err: fmt.Errorf("begin building: %w", err)}
					return
				}
				outcomes[idx] = outcome{fence: fence}
			}(w)
		}
		beginRelease.Done()
		beginDone.Wait()

		for idx, o := range outcomes {
			if o.err != nil {
				t.Fatalf("round %d worker %d: %v", round, idx, o.err)
			}
		}

		var finalizeRelease, finalizeDone sync.WaitGroup
		finalizeRelease.Add(1)
		for w := 0; w < workers; w++ {
			finalizeDone.Add(1)
			go func(idx int) {
				defer finalizeDone.Done()
				finalizeRelease.Wait()
				won, err := store.FinalizeReady(ctx, scopeID, genID, identity, projectionRevision, outcomes[idx].fence)
				if err != nil {
					outcomes[idx].err = fmt.Errorf("finalize ready: %w", err)
					return
				}
				outcomes[idx].won = won
			}(w)
		}
		finalizeRelease.Done()
		finalizeDone.Wait()

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
