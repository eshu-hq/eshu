// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// supportsLiveFactIDs returns the fact ids of every live (non-tombstoned)
// finding fact of the test's (scope, generation), sorted.
func supportsLiveFactIDs(t *testing.T, ctx context.Context, db *sql.DB, scope, generation string) []string {
	t.Helper()
	return supportsLiveQuery(t, ctx, db, `
SELECT fact_id FROM fact_records
WHERE scope_id = $1 AND generation_id = $2 AND fact_kind = $3 AND is_tombstone = FALSE
ORDER BY fact_id`, scope, generation, facts.ReducerSupplyChainImpactFindingFactKind)
}

// supportsLiveSupportIDs returns the fact ids of the supports of the (scope,
// generation), sorted.
func supportsLiveSupportIDs(t *testing.T, ctx context.Context, db *sql.DB, scope, generation string) []string {
	t.Helper()
	return supportsLiveQuery(t, ctx, db, `
SELECT fact_id FROM supply_chain_impact_finding_supports
WHERE scope_id = $1 AND generation_id = $2 ORDER BY fact_id`, scope, generation)
}

func supportsLiveQuery(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	defer func() { _ = rows.Close() }()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate: %v", err)
	}
	return out
}

// supportsLiveXminSum returns the sum of the supports' row-version ids, so a
// replay that rewrites any row changes it.
func supportsLiveXminSum(t *testing.T, ctx context.Context, db *sql.DB) int64 {
	t.Helper()
	var sum int64
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(sum(xmin::text::bigint), 0) FROM supply_chain_impact_finding_supports`).Scan(&sum); err != nil {
		t.Fatalf("sum xmin: %v", err)
	}
	return sum
}

func supportsLiveAssertMirror(t *testing.T, ctx context.Context, db *sql.DB, scope, generation, step string) {
	t.Helper()
	want := supportsLiveFactIDs(t, ctx, db, scope, generation)
	got := supportsLiveSupportIDs(t, ctx, db, scope, generation)
	if !slices.Equal(want, got) {
		t.Fatalf("%s: supports = %v, live facts = %v: a support must exist exactly for each live fact", step, got, want)
	}
	// The payload is stored verbatim: byte-equal to the fact payload for every
	// support (the supports table becomes the only copy in PR-C, so a stripped
	// key is lost data).
	var different int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM supply_chain_impact_finding_supports AS s
JOIN fact_records AS f ON f.fact_id = s.fact_id
WHERE s.scope_id = $1 AND s.generation_id = $2 AND s.payload::text IS DISTINCT FROM f.payload::text`, scope, generation).Scan(&different); err != nil {
		t.Fatalf("%s: compare payloads: %v", step, err)
	}
	if different != 0 {
		t.Fatalf("%s: %d support payloads differ from their fact payload: the payload must be stored verbatim", step, different)
	}
}

// TestSupplyChainImpactSupportsFollowTheWriterLive proves the PR-B1 maintainer on
// PostgreSQL through the real writer: one support per live fact after every
// pass, a replay rewrites nothing, the replace-set retraction removes the
// supports of the facts it tombstones, a partial-evidence pass removes nothing,
// a revived fact gets its support back, the derived columns match the winners
// built from the same facts, and deleting the generation removes the supports.
func TestSupplyChainImpactSupportsFollowTheWriterLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	writer := replaceSetWriter(db)
	scope, generation := replaceSetLiveScope, replaceSetLiveGeneration
	finding := func(n int) supplychaincore.SupplyChainImpactFinding {
		return replaceSetFinding(fmt.Sprintf("repository:r_10880_%d", n))
	}
	write := func(intent string, findings ...supplychaincore.SupplyChainImpactFinding) {
		t.Helper()
		if _, err := writer.WriteSupplyChainImpactFindings(ctx, replaceSetWrite(intent, findings...)); err != nil {
			t.Fatalf("%s: %v", intent, err)
		}
	}

	write("intent:10880:supports-1", finding(1), finding(2), finding(3))
	supportsLiveAssertMirror(t, ctx, db, scope, generation, "first pass")
	if got := len(supportsLiveSupportIDs(t, ctx, db, scope, generation)); got != 3 {
		t.Fatalf("supports after the first pass = %d, want 3", got)
	}

	// Derived columns equal the winners built from the same facts.
	if err := postgres.NewSupplyChainImpactWinnersStore(postgres.SQLDB{DB: db}).RebuildAllWinners(ctx); err != nil {
		t.Fatalf("RebuildAllWinners: %v", err)
	}
	var mismatched int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM supply_chain_impact_canonical_winners AS w
JOIN supply_chain_impact_finding_supports AS s ON s.fact_id = w.winner_fact_id
WHERE (w.canonical_key, w.finding_id, w.priority_score, w.severity_bucket, w.repository_id, w.impact_status, w.cve_id, w.package_id, w.priority_bucket, w.source_confidence)
      IS DISTINCT FROM
      (s.canonical_key, s.finding_id, s.priority_score, s.severity_bucket, s.repository_id, s.impact_status, s.cve_id, s.package_id, s.priority_bucket, s.source_confidence)`).Scan(&mismatched); err != nil {
		t.Fatalf("compare supports with winners: %v", err)
	}
	var winners int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM supply_chain_impact_canonical_winners`).Scan(&winners); err != nil || winners != 3 {
		t.Fatalf("winners = %d (err %v), want 3 to compare against", winners, err)
	}
	if mismatched != 0 {
		t.Fatalf("%d supports differ from the winner built from the same fact", mismatched)
	}

	// Replay: the same set under the same intent id changes no fact payload, so it
	// writes no support row version. A new intent id changes the verbatim
	// payload and rewrites the support, as it rewrites the fact.
	before := supportsLiveXminSum(t, ctx, db)
	write("intent:10880:supports-1", finding(1), finding(2), finding(3))
	if after := supportsLiveXminSum(t, ctx, db); after != before {
		t.Fatalf("a replay of the same set rewrote support rows (xmin sum %d to %d)", before, after)
	}

	// Replace set: dropping finding 3 tombstones its fact and removes its support.
	write("intent:10880:supports-2", finding(1), finding(2))
	supportsLiveAssertMirror(t, ctx, db, scope, generation, "replace set")
	if got := len(supportsLiveSupportIDs(t, ctx, db, scope, generation)); got != 2 {
		t.Fatalf("supports after the replace set = %d, want 2", got)
	}

	// A partial-evidence pass skips the retraction, so it removes no support.
	partial := replaceSetWrite("intent:10880:supports-partial", finding(1))
	partial.PartialEvidence = true
	if _, err := writer.WriteSupplyChainImpactFindings(ctx, partial); err != nil {
		t.Fatalf("partial pass: %v", err)
	}
	supportsLiveAssertMirror(t, ctx, db, scope, generation, "partial pass")
	if got := len(supportsLiveSupportIDs(t, ctx, db, scope, generation)); got != 2 {
		t.Fatalf("supports after a partial pass = %d, want 2 (it retracts nothing)", got)
	}

	// Revival: a later pass derives finding 3 again; its support returns.
	write("intent:10880:supports-revive", finding(1), finding(2), finding(3))
	supportsLiveAssertMirror(t, ctx, db, scope, generation, "revived fact")
	if got := len(supportsLiveSupportIDs(t, ctx, db, scope, generation)); got != 3 {
		t.Fatalf("supports after the revival = %d, want 3", got)
	}

	// The generation's removal takes its supports with it.
	if _, err := db.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id = NULL WHERE scope_id = $1`, scope); err != nil {
		t.Fatalf("clear active generation: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM scope_generations WHERE generation_id = $1`, generation); err != nil {
		t.Fatalf("delete generation: %v", err)
	}
	var left int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM supply_chain_impact_finding_supports WHERE generation_id = $1`, generation).Scan(&left); err != nil || left != 0 {
		t.Fatalf("supports left after the generation was deleted = %d (err %v), want 0 (FK cascade)", left, err)
	}
}

// TestSupplyChainImpactSupportsSurviveRacingWritersLive races writers with
// different finding sets and fencing tokens over one (scope, generation), and a
// concurrent winners resweep loop, and requires the supports to equal the live
// facts after every round with no error beyond the retryable superseded pass.
// The supports write runs under the conflict-domain lock after the fact insert
// and before the dirty-log capture, so the lock order is domain lock, fact rows,
// support rows, dirty log; the resweep takes its own advisory lock and writes
// only winners, so no cycle can form and no deadlock may surface.
func TestSupplyChainImpactSupportsSurviveRacingWritersLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	replaceSetSeedSecondScope(t, ctx, db)
	writer := replaceSetWriter(db)
	winners := postgres.NewSupplyChainImpactWinnersStore(postgres.SQLDB{DB: db})

	const workers, rounds = 8, 5
	stop := make(chan struct{})
	sweepErr := make(chan error, 1)
	var sweeps sync.WaitGroup
	sweeps.Add(1)
	go func() {
		defer sweeps.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if err := winners.RebuildAllWinners(ctx); err != nil {
				select {
				case sweepErr <- err:
				default:
				}
				return
			}
		}
	}()
	t.Cleanup(func() { close(stop); sweeps.Wait() })

	for round := 0; round < rounds; round++ {
		var wg sync.WaitGroup
		errs := make(chan error, workers*2)
		for i := 0; i < workers; i++ {
			findings := []supplychaincore.SupplyChainImpactFinding{}
			for j := 0; j <= i; j++ {
				findings = append(findings, replaceSetFinding(fmt.Sprintf("repository:r_10880_race_%d", j)))
			}
			// Half the workers write the second scope with the same finding set,
			// so the same canonical keys are written from two scopes at once.
			for _, scoped := range []bool{false, true} {
				write := replaceSetWrite(fmt.Sprintf("intent:10880:race-%d-%d-%v", round, i, scoped), findings...)
				if scoped {
					write.ScopeID, write.GenerationID = replaceSetSecondScope, replaceSetSecondGeneration
				}
				wg.Add(1)
				go func() {
					defer wg.Done()
					if _, err := writer.WriteSupplyChainImpactFindings(ctx, write); err != nil {
						errs <- err
					}
				}()
			}
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			var classified interface{ FailureClass() string }
			if !errors.As(err, &classified) || classified.FailureClass() != supplychaincore.SupplyChainImpactWriteSupersededFailureClass {
				t.Fatalf("round %d: writer failed with %v, want success or the retryable superseded error", round, err)
			}
		}
		supportsLiveAssertMirror(t, ctx, db, replaceSetLiveScope, replaceSetLiveGeneration, fmt.Sprintf("round %d first scope", round))
		supportsLiveAssertMirror(t, ctx, db, replaceSetSecondScope, replaceSetSecondGeneration, fmt.Sprintf("round %d second scope", round))
		select {
		case err := <-sweepErr:
			t.Fatalf("a concurrent winners resweep failed (a lock cycle with the writers?): %v", err)
		default:
		}
	}
}
