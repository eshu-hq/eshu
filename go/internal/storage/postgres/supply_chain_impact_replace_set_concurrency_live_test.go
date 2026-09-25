// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestSupplyChainImpactWriterSerializesOverlappingPassesLive proves the
// conflict-domain lock. Two passes for the same (scope, generation) overlap
// the worst way under Read Committed: pass 1 has upserted and retracted but
// not committed when pass 2 starts. Without a per-(scope, generation) lock,
// pass 2's retract cannot see pass 1's uncommitted rows, both commit, and the
// active set is the union of two different passes -- truth neither pass
// derived. With the lock, pass 2 waits for pass 1 to commit, its retract then
// sees pass 1's rows, and the final set is exactly pass 2's.
func TestSupplyChainImpactWriterSerializesOverlappingPassesLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	replaceSetSeedSecondScope(t, ctx, db)

	commitGate := make(chan struct{})
	commitReached := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(commitGate) }) }
	// A failing assertion must not leave pass 1 parked mid-transaction
	// holding the lock the schema cleanup needs.
	t.Cleanup(release)
	gated := reducer.PostgresSupplyChainImpactWriter{
		DB: gatedCommitBeginner{
			inner:   replaceSetWriter(db).DB,
			reached: commitReached,
			gate:    commitGate,
		},
		Now: func() time.Time { return replaceSetLiveNow },
	}
	plain := replaceSetWriter(db)

	firstDone := make(chan error, 1)
	go func() {
		_, err := gated.WriteSupplyChainImpactFindings(ctx,
			replaceSetWrite("intent:6831:overlap-1", replaceSetFinding("")))
		firstDone <- err
	}()
	<-commitReached

	// A pass for a DIFFERENT scope must not wait on this conflict domain.
	otherScope := replaceSetWrite("intent:6831:other-scope", replaceSetFinding(replaceSetLiveRepository))
	otherScope.ScopeID = replaceSetSecondScope
	otherScope.GenerationID = replaceSetSecondGeneration
	otherCtx, otherCancel := context.WithTimeout(ctx, 5*time.Second)
	defer otherCancel()
	if _, err := plain.WriteSupplyChainImpactFindings(otherCtx, otherScope); err != nil {
		t.Fatalf("other-scope pass blocked or failed while pass 1 held its lock: %v", err)
	}

	secondDone := make(chan error, 1)
	go func() {
		_, err := plain.WriteSupplyChainImpactFindings(ctx,
			replaceSetWrite("intent:6831:overlap-2", replaceSetFinding(replaceSetLiveRepository)))
		secondDone <- err
	}()
	select {
	case err := <-secondDone:
		t.Fatalf("pass 2 finished (err=%v) while pass 1 was uncommitted; the (scope, generation) lock is missing", err)
	case <-time.After(750 * time.Millisecond):
	}
	if waiting := replaceSetAdvisoryWaiters(t, ctx, db); waiting != 1 {
		t.Fatalf("ungranted advisory locks = %d, want 1 (pass 2 waiting on the conflict-domain lock)", waiting)
	}

	release()
	if err := <-firstDone; err != nil {
		t.Fatalf("pass 1: %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("pass 2: %v", err)
	}
	active := replaceSetActiveRepositories(t, ctx, db)
	if len(active) != 1 || active[0] != replaceSetLiveRepository {
		t.Fatalf("active repositories = %q, want exactly pass 2's anchored finding", active)
	}
}

// TestSupplyChainImpactWriterConcurrentPassesConvergeLive races many passes
// with different finding sets over one (scope, generation). Every pass must
// succeed (no deadlock, no serialization failure) and the surviving active
// set must be exactly one pass's complete set -- never a union or empty mix.
func TestSupplyChainImpactWriterConcurrentPassesConvergeLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	writer := replaceSetWriter(db)

	const workers, rounds = 8, 6
	candidates := make([]reducer.SupplyChainImpactWrite, workers)
	expected := make([][]string, workers)
	for i := range candidates {
		findings := make([]reducer.SupplyChainImpactFinding, 0, i+1)
		for j := 0; j <= i; j++ {
			repositoryID := fmt.Sprintf("repository:r_6831_w%d_%d", i, j)
			findings = append(findings, replaceSetFinding(repositoryID))
			expected[i] = append(expected[i], repositoryID)
		}
		slices.Sort(expected[i])
		candidates[i] = replaceSetWrite(fmt.Sprintf("intent:6831:race-%d", i), findings...)
	}

	for round := 0; round < rounds; round++ {
		var wg sync.WaitGroup
		errs := make(chan error, workers)
		for i := range candidates {
			wg.Add(1)
			go func(write reducer.SupplyChainImpactWrite) {
				defer wg.Done()
				if _, err := writer.WriteSupplyChainImpactFindings(ctx, write); err != nil {
					errs <- err
				}
			}(candidates[i])
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatalf("round %d concurrent pass failed: %v", round, err)
		}
		active := replaceSetActiveRepositories(t, ctx, db)
		if !slices.ContainsFunc(expected, func(set []string) bool { return slices.Equal(set, active) }) {
			t.Fatalf("round %d active set %q is not any single pass's complete set", round, active)
		}
	}
}

const (
	replaceSetSecondScope      = "scope:6831:replace-set:second"
	replaceSetSecondGeneration = "generation:6831:replace-set:second"
)

func replaceSetSeedSecondScope(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload)
VALUES ($1, 'vulnerability_intelligence', 'synthetic', $1, 'synthetic', $1, $2, $2, 'active', '{}'::jsonb)`,
		replaceSetSecondScope, replaceSetLiveNow,
	); err != nil {
		t.Fatalf("seed second scope: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at,
  status, activated_at, payload)
VALUES ($1, $2, 'synthetic', $3, $3, 'active', $3, '{}'::jsonb)`,
		replaceSetSecondGeneration, replaceSetSecondScope, replaceSetLiveNow,
	); err != nil {
		t.Fatalf("seed second generation: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`,
		replaceSetSecondGeneration, replaceSetSecondScope,
	); err != nil {
		t.Fatalf("activate second generation: %v", err)
	}
}

func replaceSetAdvisoryWaiters(t *testing.T, ctx context.Context, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted`,
	).Scan(&count); err != nil {
		t.Fatalf("read pg_locks: %v", err)
	}
	return count
}

// gatedCommitBeginner wraps the production beginner so a test can hold one
// pass open between its last statement and its commit.
type gatedCommitBeginner struct {
	inner   reducer.SupplyChainImpactBeginner
	reached chan<- struct{}
	gate    <-chan struct{}
}

func (b gatedCommitBeginner) BeginSupplyChainImpactTx(ctx context.Context) (reducer.SupplyChainImpactTx, error) {
	tx, err := b.inner.BeginSupplyChainImpactTx(ctx)
	if err != nil {
		return nil, err
	}
	return gatedCommitTx{SupplyChainImpactTx: tx, reached: b.reached, gate: b.gate}, nil
}

type gatedCommitTx struct {
	reducer.SupplyChainImpactTx
	reached chan<- struct{}
	gate    <-chan struct{}
}

func (t gatedCommitTx) Commit() error {
	close(t.reached)
	<-t.gate
	return t.SupplyChainImpactTx.Commit()
}
