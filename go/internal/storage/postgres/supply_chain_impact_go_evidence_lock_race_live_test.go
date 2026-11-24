// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11327 real PostgreSQL race proofs of the go-evidence lock: 40 rounds of the fence reopening the work item of a pass already in flight (zero deadlocks), a successor ack that commits while a pass holds the shared lock for 3 s and ends on the successor, a multi-repository pass with fences on two of its go scopes in parallel, the recheck covering exactly the pairs the loader resolved, and 40 free-interleaving rounds whose invariant is that no support derived from a retracted generation survives without a reopened holder
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu-sdk/factschema"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/coordination"
)

// goEvidenceChain is one holder scope whose findings join Go evidence from one go scope
// per repository, seeded the way seedGoFenceChain seeds the single-repository chain.
type goEvidenceChain struct {
	holder      string
	item        string
	advisory    string
	goScopes    []string
	generations []string
}

// seedGoEvidenceChain seeds a holder scope with one package consumption fact per
// repository and an active go-reachability scope per repository holding not_called
// evidence. tag keeps every id unique within one database.
func seedGoEvidenceChain(c *convergence, tag string, repositories int) goEvidenceChain {
	c.t.Helper()
	chainSeed := goEvidenceChain{
		holder:   "scope:11327:holder:" + tag,
		advisory: "GO-2026-" + strings.ToUpper(tag),
	}
	module := "example.com/ge-" + tag
	pkg := "gomod://proxy.golang.org/" + module
	fanOutSeedScope(c.t, c.ctx, c.db, chainSeed.holder, "g1")
	holderGeneration := fanOutGen(chainSeed.holder, "g1")
	envelopes := []facts.Envelope{
		c.envelope(chainSeed.holder, holderGeneration, "fact:11327:"+tag+":cve", chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": chainSeed.advisory, "cve_id": chainSeed.advisory, "source": "osv", "cvss_score": 7.5,
			"aliases": []any{chainSeed.advisory},
		}),
		c.envelope(chainSeed.holder, holderGeneration, "fact:11327:"+tag+":affected", chain.VulnerabilityAffectedPackageFactKind, map[string]any{
			"advisory_id": chainSeed.advisory, "cve_id": chainSeed.advisory, "package_id": pkg, "ecosystem": "gomod",
			"package_name": module, "purl": "pkg:golang/" + module, "fixed_versions": []any{"1.2.0"},
			"affected_ranges": []any{map[string]any{"type": "SEMVER", "events": []any{
				map[string]any{"introduced": "0"}, map[string]any{"fixed": "1.2.0"},
			}}},
		}),
	}
	for i := 0; i < repositories; i++ {
		repository := fmt.Sprintf("repository:r_11327%s%d", tag, i)
		envelopes = append(envelopes, c.envelope(chainSeed.holder, holderGeneration, fmt.Sprintf("fact:11327:%s:consumption:%d", tag, i),
			factschema.FactKindReducerPackageConsumptionCorrelation, map[string]any{
				"package_id": pkg, "relationship_kind": "consumption", "repository_id": repository,
				"dependency_range": "v1.1.0", "canonical_writes": 1, "evidence_fact_ids": []string{"fact:11327:" + tag + ":manifest"},
			}))
	}
	c.upsert(envelopes...)
	chainSeed.item = fanOutSeedItem(c.t, c.ctx, c.db, chainSeed.holder, "g1")
	for i := 0; i < repositories; i++ {
		repository := fmt.Sprintf("repository:r_11327%s%d", tag, i)
		goScope := "go-reachability://" + repository
		fanOutSeedScope(c.t, c.ctx, c.db, goScope, "g1")
		generation := fanOutGen(goScope, "g1")
		c.upsert(
			c.envelope(goScope, generation, fmt.Sprintf("fact:11327:%s:module:%d", tag, i), chain.VulnerabilityGoModuleEvidenceFactKind, map[string]any{
				"repository_id": repository, "relative_path": "go.mod", "module_path": module,
				"required_version": "v1.1.0", "indirect": false, "replacement_path": "", "replacement_version": "",
				"ecosystem": "go-module", "package_id": "go-module/proxy.golang.org/" + module,
				"package_name": module, "line_number": 5,
			}),
			c.envelope(goScope, generation, fmt.Sprintf("fact:11327:%s:reach:%d", tag, i), chain.VulnerabilityGoCallReachabilityFactKind, map[string]any{
				"repository_id": repository, "osv_id": chainSeed.advisory, "deepest_module": module,
				"deepest_package": "", "deepest_symbol": "", "reachability_level": "not_called",
				"trace": []map[string]string{{"module": module, "version": "v1.1.0"}},
			}),
		)
		chainSeed.goScopes = append(chainSeed.goScopes, goScope)
		chainSeed.generations = append(chainSeed.generations, generation)
	}
	return chainSeed
}

// holderVerdictNotCalled reports whether the holder's active finding still carries the
// govulncheck not_called verdict.
func holderVerdictNotCalled(c *convergence, holder string) bool {
	c.t.Helper()
	var count int
	if err := c.db.QueryRowContext(c.ctx, `
SELECT count(*) FROM fact_records f
JOIN ingestion_scopes s ON s.scope_id = f.scope_id AND s.active_generation_id = f.generation_id
WHERE f.scope_id = $1 AND f.fact_kind = 'reducer_supply_chain_impact_finding' AND NOT f.is_tombstone
  AND f.payload->>'runtime_reachability' = $2`, holder, string(supplychaincore.GoVulnReachabilityNotCalled)).Scan(&count); err != nil {
		c.t.Fatalf("read the holder verdict: %v", err)
	}
	return count > 0
}

// goEvidenceInvariantViolations is the #11327 invariant over one database: the holders
// whose active finding still carries the not_called verdict while a go scope of theirs
// has had its active pointer retracted by a committed fence, after the queue drained
// (so nothing will re-derive them). It reads the verdict instead of the lock, so a
// regression that skips the lock still fails here.
func goEvidenceInvariantViolations(c *convergence, chains []goEvidenceChain) []string {
	c.t.Helper()
	var violations []string
	for _, chainSeed := range chains {
		if holderVerdictNotCalled(c, chainSeed.holder) {
			var retracted int
			if err := c.db.QueryRowContext(c.ctx, `
SELECT count(*) FROM ingestion_scopes WHERE scope_id = ANY($1) AND active_generation_id IS NULL`, chainSeed.goScopes).Scan(&retracted); err != nil {
				c.t.Fatalf("read the go scopes: %v", err)
			}
			if retracted > 0 {
				violations = append(violations, chainSeed.holder)
			}
		}
	}
	return violations
}

// retryFence runs the fence until it commits, retrying a lock timeout, and fails on any
// other error (a deadlock among them).
func retryFence(ctx context.Context, t *testing.T, db *sql.DB, scope, generation string) {
	t.Helper()
	for attempt := 0; attempt < 40; attempt++ {
		_, err := goEvidenceFence(ctx, db, scope, generation, nil)
		switch {
		case err == nil:
			return
		case coordination.IsLockNotAvailable(err):
			time.Sleep(50 * time.Millisecond)
		default:
			t.Errorf("fence of %s failed with a non-timeout error: %v", scope, err)
			return
		}
	}
	t.Errorf("fence of %s never committed in 40 attempts", scope)
}

// TestGoEvidenceFenceReopensAnInFlightPassWithoutDeadlockLive is ruling 4.3 for the
// lock-last position (the amendment's gate 2): the pass's own scope is already a holder
// (it has supports from an earlier pass, and its owed marker updates its own work item,
// the row the fence's reopen updates) while the fence reopens that scope's work item. Three
// interleavings, 40 rounds in all: the fence started right before the pass requests the lock
// and awaited, started and not awaited (the pass may wait on a fence that holds the
// exclusive lock), and started while the pass is paused holding the lock. Zero deadlocks
// (40P01), every fence commits, and every pass either commits or is refused with the typed
// class.
func TestGoEvidenceFenceReopensAnInFlightPassWithoutDeadlockLive(t *testing.T) {
	c := newGoEvidenceConvergence(t)
	plainWriter := c.handler.Writer // every round starts from the production writer, never a prior round's seam
	for round := 0; round < 40; round++ {
		c.handler.Writer = plainWriter
		seed := seedGoEvidenceChain(c, fmt.Sprintf("dl%02d", round), 1)
		c.drain() // the first pass: the scope becomes a holder with supports.
		settleSupplyChainImpactSupports(c.t, c.ctx, c.db)
		c.rerun(seed.holder)

		var wg sync.WaitGroup
		launch := func() {
			wg.Add(1)
			go func() {
				defer wg.Done()
				retryFence(c.ctx, c.t, c.db, seed.goScopes[0], seed.generations[0])
			}()
		}
		beginner := recheckHookBeginner{inner: postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: c.db}}}
		switch round % 3 {
		case 0:
			beginner.beforeLock = func(context.Context) { launch(); wg.Wait() }
		case 1:
			beginner.beforeLock = func(context.Context) { launch() }
		default:
			beginner.hook = func(ctx context.Context) { launch(); waitForAdvisoryWaiter(ctx, c.t, c.db, 1) }
		}
		var passErr error
		c.handler.Writer = hookingWriter{
			inner: supplychaincore.PostgresSupplyChainImpactWriter{DB: beginner},
			after: func(write supplychaincore.SupplyChainImpactWrite, err error) {
				if write.ScopeID == seed.holder && err != nil {
					passErr = err
				}
			},
		}
		c.drain() // the in-flight pass.
		wg.Wait()
		c.handler.Writer = plainWriter // the follow-up drain must not re-fire the round's seam
		c.drain()                      // the reopened holder.
		if passErr != nil {
			var changed *supplychaincore.SupplyChainImpactGoEvidenceChangedError
			if !errors.As(passErr, &changed) {
				t.Fatalf("round %d: the pass failed with %v, want it committed or refused with the typed class (a deadlock reads 40P01 here)", round, passErr)
			}
		}
		if t.Failed() {
			t.Fatalf("round %d failed (deadlock or fence error above)", round)
		}
	}
}

// TestGoEvidenceAckCommitsWhilePassHoldsTheSharedLockLive is the central claim of design
// B (ruling 4.4): the ack of a successor takes no advisory lock, so it commits while a
// pass holds the shared lock for 3 s, and the race of pass, fence and ack ends on the
// successor with no deadlock. The ack here is the production statements (scope pointer
// to the successor, the old generation superseded, the successor activated) in one
// transaction taking the scope row first, as ProjectorQueue.Ack does (projector_queue_ack.go
// lock order).
func TestGoEvidenceAckCommitsWhilePassHoldsTheSharedLockLive(t *testing.T) {
	c := newGoEvidenceConvergence(t)
	plainWriter := c.handler.Writer // every round starts from the production writer, never a prior round's seam
	for round := 0; round < 40; round++ {
		c.handler.Writer = plainWriter
		seed := seedGoEvidenceChain(c, fmt.Sprintf("ak%02d", round), 1)
		successor := seed.goScopes[0] + "/g2"
		hold := 50 * time.Millisecond
		if round == 0 {
			hold = 3 * time.Second
		}
		var wg sync.WaitGroup
		var ackLatency time.Duration
		var ackErr error
		c.handler.Writer = supplychaincore.PostgresSupplyChainImpactWriter{
			DB: recheckHookBeginner{
				inner: postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: c.db}},
				hook: func(ctx context.Context) {
					wg.Add(2)
					go func() {
						defer wg.Done()
						retryFence(c.ctx, c.t, c.db, seed.goScopes[0], seed.generations[0])
					}()
					go func() {
						defer wg.Done()
						started := time.Now()
						ackErr = ackSuccessor(c.ctx, c.db, seed.goScopes[0], seed.generations[0], successor)
						ackLatency = time.Since(started)
					}()
					time.Sleep(hold)
				},
			},
		}
		c.drain()
		wg.Wait()
		if ackErr != nil {
			t.Fatalf("round %d: ack failed: %v", round, ackErr)
		}
		if round == 0 && ackLatency > time.Second {
			t.Fatalf("the ack took %v while the pass held the shared lock for 3 s; it must not wait on the advisory lock", ackLatency)
		}
		var pointer sql.NullString
		if err := c.db.QueryRowContext(c.ctx, `SELECT active_generation_id FROM ingestion_scopes WHERE scope_id = $1`, seed.goScopes[0]).Scan(&pointer); err != nil {
			t.Fatalf("read the pointer: %v", err)
		}
		if pointer.String != successor {
			t.Fatalf("round %d: pointer = %q, want the successor %q (fence and ack must end on the successor)", round, pointer.String, successor)
		}
		if t.Failed() {
			t.Fatalf("round %d failed above", round)
		}
	}
}

// ackSuccessor commits a successor generation the way the projector ack does for a scope
// whose active generation is old: the scope row first, then the generations.
func ackSuccessor(ctx context.Context, db *sql.DB, scope, old, successor string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	statements := []struct {
		query string
		args  []any
	}{
		{`UPDATE ingestion_scopes SET status = 'active', active_generation_id = $2, ingested_at = now() WHERE scope_id = $1`, []any{scope, successor}},
		{`UPDATE scope_generations SET status = 'superseded', superseded_at = now() WHERE scope_id = $1 AND generation_id = $2`, []any{scope, old}},
		{`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
VALUES ($2, $1, 'synthetic', now(), now(), 'active', now())`, []any{scope, successor}},
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement.query, statement.args...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// TestGoEvidenceMultiRepositoryPassWithParallelFencesLive is ruling 4.6: one pass joins
// Go evidence from two go scopes, so it takes two shared locks in sorted order, while a
// fence runs on each go scope in parallel. Zero deadlocks, every fence commits, and the
// holder ends re-derived.
func TestGoEvidenceMultiRepositoryPassWithParallelFencesLive(t *testing.T) {
	c := newGoEvidenceConvergence(t)
	plainWriter := c.handler.Writer // every round starts from the production writer, never a prior round's seam
	for round := 0; round < 10; round++ {
		c.handler.Writer = plainWriter
		seed := seedGoEvidenceChain(c, fmt.Sprintf("mr%02d", round), 2)
		var wg sync.WaitGroup
		c.handler.Writer = supplychaincore.PostgresSupplyChainImpactWriter{
			DB: recheckHookBeginner{
				inner: postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: c.db}},
				hook: func(ctx context.Context) {
					for i := range seed.goScopes {
						wg.Add(1)
						go func() {
							defer wg.Done()
							retryFence(c.ctx, c.t, c.db, seed.goScopes[i], seed.generations[i])
						}()
					}
					waitForAdvisoryWaiter(ctx, c.t, c.db, 2)
				},
			},
		}
		c.drain()
		wg.Wait()
		c.handler.Writer = plainWriter // the follow-up drain must not re-fire the round's seam
		c.drain()
		if violations := goEvidenceInvariantViolations(c, []goEvidenceChain{seed}); len(violations) > 0 {
			t.Fatalf("round %d: holders still carrying a retracted verdict: %v", round, violations)
		}
		if t.Failed() {
			t.Fatalf("round %d failed above", round)
		}
	}
}

// TestGoEvidenceRecheckCoversExactlyTheLoadedPairsLive is ruling 4.7 on the real loader and
// writer: three repositories asked, one go scope has no active generation, so the loader
// resolves two pairs and the writer takes two shared locks and rechecks two pairs.
func TestGoEvidenceRecheckCoversExactlyTheLoadedPairsLive(t *testing.T) {
	c := newGoEvidenceConvergence(t)
	seed := seedGoEvidenceChain(c, "pr", 2)
	dormantRepository := "repository:r_11327prdormant"
	dormantScope := "go-reachability://" + dormantRepository
	fanOutSeedScope(c.t, c.ctx, c.db, dormantScope, "g1")
	if _, err := c.db.ExecContext(c.ctx, `UPDATE ingestion_scopes SET active_generation_id = NULL WHERE scope_id = $1`, dormantScope); err != nil {
		t.Fatalf("clear the dormant pointer: %v", err)
	}
	repositories := []string{"repository:r_11327pr0", "repository:r_11327pr1", dormantRepository}
	evidence, err := postgres.NewFactStore(postgres.SQLDB{DB: c.db}).ListActiveGoReachabilityEvidence(c.ctx,
		supplychaincore.GoReachabilityFactFilter{RepositoryIDs: repositories})
	if err != nil {
		t.Fatalf("ListActiveGoReachabilityEvidence() error = %v", err)
	}
	if len(evidence.Generations) != 2 {
		t.Fatalf("resolved pairs = %v, want the two scopes with an active generation of the three asked", evidence.Generations)
	}

	recorder := &recordingBeginner{inner: postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: c.db}}}
	write := fenceWrite("intent:11327:pairs", seed.holder, fanOutGen(seed.holder, "g1"), fallbackFinding("CVE-2026-9101", "pkg:npm/a@1"))
	write.GoEvidence = evidence.Generations
	if _, err := (supplychaincore.PostgresSupplyChainImpactWriter{DB: recorder, Now: time.Now}).WriteSupplyChainImpactFindings(c.ctx, write); err != nil {
		t.Fatalf("write error = %v", err)
	}
	locks, rechecked := 0, 0
	for _, call := range recorder.calls {
		switch {
		case call.query == supplychaincore.SupplyChainImpactGoEvidenceSharedLockQuery:
			locks++
		case strings.Contains(call.query, "AS pair(scope_id, generation_id)"):
			if ids, ok := call.args[0].([]string); ok {
				rechecked = len(ids)
			}
		}
	}
	if locks != 2 || rechecked != 2 {
		t.Fatalf("shared locks = %d, rechecked pairs = %d, want 2 and 2 (three asked, two resolved)", locks, rechecked)
	}
}

// TestGoEvidenceFreeInterleavingLeavesNoStaleVerdictLive is ruling 4.8 and issue AC3: 40
// rounds in which the fence and the holder's first pass race with no ordering imposed beyond
// one of three starting points (fence before the write, fence launched from the pass's
// own write and left to race, fence launched after the shared lock is held). The
// invariant: after the queue drains, no holder carries the not_called verdict of a go
// generation that a committed fence retracted.
func TestGoEvidenceFreeInterleavingLeavesNoStaleVerdictLive(t *testing.T) {
	c := newGoEvidenceConvergence(t)
	var chains []goEvidenceChain
	plainWriter := c.handler.Writer // every round starts from the production writer, never a prior round's seam
	for round := 0; round < 40; round++ {
		c.handler.Writer = plainWriter
		seed := seedGoEvidenceChain(c, fmt.Sprintf("fi%02d", round), 1)
		chains = append(chains, seed)
		var wg sync.WaitGroup
		launch := func() {
			wg.Add(1)
			go func() {
				defer wg.Done()
				retryFence(c.ctx, c.t, c.db, seed.goScopes[0], seed.generations[0])
			}()
		}
		switch round % 3 {
		case 0:
			c.handler.Writer = hookingWriter{inner: plainWriter, before: func(write supplychaincore.SupplyChainImpactWrite) {
				if write.ScopeID == seed.holder {
					launch()
					wg.Wait()
				}
			}}
		case 1:
			c.handler.Writer = hookingWriter{inner: plainWriter, before: func(write supplychaincore.SupplyChainImpactWrite) {
				if write.ScopeID == seed.holder {
					launch()
				}
			}}
		default:
			c.handler.Writer = supplychaincore.PostgresSupplyChainImpactWriter{
				DB: recheckHookBeginner{
					inner: postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: c.db}},
					hook: func(ctx context.Context) {
						launch()
						waitForAdvisoryWaiter(ctx, c.t, c.db, 1)
					},
				},
			}
		}
		c.drain()
		wg.Wait()
		c.handler.Writer = plainWriter // the follow-up drain must not re-fire the round's seam
		c.drain()
		if t.Failed() {
			t.Fatalf("round %d failed above", round)
		}
	}
	if violations := goEvidenceInvariantViolations(c, chains); len(violations) > 0 {
		t.Fatalf("holders still carrying a not_called verdict of a retracted generation: %v", violations)
	}
}
