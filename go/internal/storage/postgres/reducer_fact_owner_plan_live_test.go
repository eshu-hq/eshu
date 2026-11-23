// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11213 before/after cost proof for the reducer scope owner leg on disposable PostgreSQL 18: EXPLAIN (ANALYZE, BUFFERS) of a 1000-row conflicting batch and of a single-row upsert with and without the leg (the pre statements are derived from the shipped constants), and a 15-run paired median-and-spread table over three 1000-row mixes (all changed, identical replay, one foreign row) on a 200,000-row table; wall time logged not gated
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
)

const (
	reducerOwnerLeg       = "\n  AND fact_records.scope_id = EXCLUDED.scope_id"
	reducerSingleOwnerLeg = "WHERE fact_records.scope_id = EXCLUDED.scope_id\n"
)

// reducerPreBatch and reducerPreSingle are the shipped statements without the
// owner leg, derived rather than copied.
func reducerPreBatch() string {
	return strings.Replace(factwrite.BatchInsertQuery, reducerOwnerLeg, "", 1)
}

func reducerPreSingle() string {
	return strings.Replace(factwrite.SingleInsertQuery, reducerSingleOwnerLeg, "", 1)
}

// TestReducerOwnerLegPreStatementsAreDerived guards the derivation: removing the
// leg must change each statement.
func TestReducerOwnerLegPreStatementsAreDerived(t *testing.T) {
	t.Parallel()
	if reducerPreBatch() == factwrite.BatchInsertQuery || reducerPreSingle() == factwrite.SingleInsertQuery {
		t.Fatal("the owner leg is not part of a shipped statement; the pre-change derivation is a no-op")
	}
	if strings.Contains(reducerPreBatch(), "scope_id = EXCLUDED.scope_id") || strings.Contains(reducerPreSingle(), "scope_id = EXCLUDED.scope_id") {
		t.Fatal("a derived pre-change statement still carries the owner leg")
	}
}

func ownerPlanRows(prefix string, n int, scopeID string, at time.Time, tag string) []factwrite.Row {
	rows := make([]factwrite.Row, n)
	for i := range rows {
		rows[i] = ownerLiveRow(fmt.Sprintf("%s%06d", prefix, i), scopeID, "gen-plan", 5, tag, at)
	}
	return rows
}

func ownerPlanMedian(v []float64) float64 {
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	m := len(s) / 2
	if len(s)%2 == 0 {
		return (s[m-1] + s[m]) / 2
	}
	return s[m]
}

// TestReducerOwnerLegCostLive logs the plan and the paired timing of the three
// reducer statements with and without the owner leg.
func TestReducerOwnerLegCostLive(t *testing.T) {
	dsn := unchangedSkipProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #11213 cost proof")
	}
	ctx := context.Background()
	db := openUnchangedSkipSchema(t, ctx, dsn)
	store := SQLDB{DB: db}
	now := time.Date(2026, time.November, 22, 12, 0, 0, 0, time.UTC)
	seedUnchangedSkipScope(t, ctx, db, "scope-plan", "gen-plan", now)
	seedUnchangedSkipScope(t, ctx, db, "scope-other", "gen-other", now)
	const table, batch = 200000, 1000
	if err := factwrite.BatchInsertFacts(ctx, store, ownerPlanRows("bulk-", table, "scope-plan", now, "seed")); err != nil {
		t.Fatalf("seed %d rows: %v", table, err)
	}
	if err := factwrite.BatchInsertFacts(ctx, store, ownerPlanRows("foreign-", 1, "scope-other", now, "seed")); err != nil {
		t.Fatalf("seed foreign row: %v", err)
	}
	if _, err := db.ExecContext(ctx, "ANALYZE fact_records"); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	explain := func(label, statement string, args []any) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback() }()
		rows, err := tx.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+statement, args...)
		if err != nil {
			t.Fatalf("explain %s: %v", label, err)
		}
		var plan []string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatalf("scan: %v", err)
			}
			plan = append(plan, line)
		}
		_ = rows.Close()
		t.Logf("PLAN %s:\n%s", label, strings.Join(plan, "\n"))
	}
	changed := ownerPlanRows("bulk-", batch, "scope-plan", now.Add(time.Hour), "changed")
	for i := 0; i < 2; i++ {
		explain("batch pre", reducerPreBatch(), factwrite.ChunkArgs(changed))
		explain("batch post", factwrite.BatchInsertQuery, factwrite.ChunkArgs(changed))
	}
	one := ownerPlanRows("bulk-", 1, "scope-plan", now.Add(time.Hour), "one")[0]
	singleArgs := []any{
		one.FactID, one.ScopeID, one.GenerationID, one.FactKind, one.StableFactKey, one.CollectorKind,
		one.SourceConfidence, one.SourceSystem, one.SourceFactKey, nil, nil, one.ObservedAt, one.IngestedAt, false, one.Payload,
	}
	for i := 0; i < 2; i++ {
		explain("single pre", reducerPreSingle(), singleArgs)
		explain("single post", factwrite.SingleInsertQuery, singleArgs)
	}

	// Paired timing, strict alternation, order flipped each run.
	const runs, warmups = 15, 2
	mixes := []struct {
		name  string
		build func(run int) []factwrite.Row
	}{
		{"all changed", func(run int) []factwrite.Row {
			return ownerPlanRows("bulk-", batch, "scope-plan", now.Add(time.Duration(run+2)*time.Hour), fmt.Sprintf("c%d", run))
		}},
		{"identical replay", func(int) []factwrite.Row { return ownerPlanRows("bulk-", batch, "scope-plan", now, "seed") }},
		{"identical replay, 1 foreign in 1000", func(int) []factwrite.Row {
			rows := ownerPlanRows("bulk-", batch, "scope-plan", now, "seed")
			rows[batch/2] = ownerLiveRow("foreign-000000", "scope-plan", "gen-plan", 5, "intruder", now)
			return rows
		}},
	}
	for _, mix := range mixes {
		var pre, post []float64
		for run := -warmups; run < runs; run++ {
			order := []string{"pre", "post"}
			if run%2 != 0 {
				order = []string{"post", "pre"}
			}
			for _, shape := range order {
				rows := mix.build(run + warmups)
				start := time.Now()
				var err error
				if shape == "pre" {
					_, err = db.ExecContext(ctx, reducerPreBatch(), factwrite.ChunkArgs(rows)...)
				} else {
					err = factwrite.BatchInsertFacts(ctx, store, rows)
					if err != nil && strings.Contains(err.Error(), "fact_id_owner_conflict") {
						err = nil // the foreign row is the measured outcome of that mix
					}
				}
				took := time.Since(start).Seconds()
				if err != nil {
					t.Fatalf("%s %s run %d: %v", mix.name, shape, run, err)
				}
				if run < 0 {
					continue
				}
				if shape == "pre" {
					pre = append(pre, took)
				} else {
					post = append(post, took)
				}
			}
		}
		sort.Float64s(pre)
		sort.Float64s(post)
		mp, mq := ownerPlanMedian(pre), ownerPlanMedian(post)
		t.Logf("MIX %-38s pre median=%.6fs [%.6f..%.6f]  post median=%.6fs [%.6f..%.6f]  post/pre=%.3f  post median inside pre spread=%t",
			mix.name, mp, pre[0], pre[len(pre)-1], mq, post[0], post[len(post)-1], mq/mp, mq >= pre[0] && mq <= pre[len(pre)-1])
	}
}
