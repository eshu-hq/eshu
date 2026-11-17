// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/retention"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	relationshipretention "github.com/eshu-hq/eshu/go/internal/storage/postgres/retention/relationships"
)

// TestLoadGenerationRetentionRelationshipSweepStaysOffByDefault pins the
// #10823 enablement posture: an unset or unparsable switch leaves the delete
// sweep off, only an explicit true opts in, and the row cap defaults to the
// ruled 100,000 and falls back to it for an unparsable or non-positive value.
func TestLoadGenerationRetentionRelationshipSweepStaysOffByDefault(t *testing.T) {
	env := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}
	unset := loadGenerationRetentionConfig(env(nil)).Runner.Policy
	require.False(t, unset.RelationshipRetentionSweepEnabled)
	require.Equal(t, int64(100_000), unset.RelationshipRetentionRowCap)
	require.Equal(t, relationshipretention.DefaultRowCap, unset.RelationshipRetentionRowCap)

	for _, junk := range []string{"ture", "enabled", " ", "2"} {
		policy := loadGenerationRetentionConfig(env(map[string]string{
			generationRetentionRelationshipSweepEnabledEnv: junk,
		})).Runner.Policy
		require.False(t, policy.RelationshipRetentionSweepEnabled, "%q must leave deletes off", junk)
	}

	on := loadGenerationRetentionConfig(env(map[string]string{
		generationRetentionRelationshipSweepEnabledEnv: "true",
		generationRetentionRelationshipSweepRowCapEnv:  "25000",
	})).Runner.Policy
	require.True(t, on.RelationshipRetentionSweepEnabled)
	require.Equal(t, int64(25_000), on.RelationshipRetentionRowCap)

	for _, bad := range []string{"0", "-5", "lots", "1.5"} {
		policy := loadGenerationRetentionConfig(env(map[string]string{
			generationRetentionRelationshipSweepRowCapEnv: bad,
		})).Runner.Policy
		require.Equal(t, int64(100_000), policy.RelationshipRetentionRowCap, "row cap %q must fall back to the default", bad)
	}
}

// TestGenerationRetentionRunnerForWiresRelationshipSweeper pins the #10823
// wiring: a database that can begin batch transactions gets the relationship
// sweeper, so the census and its gauge run; one that cannot keeps the runner
// without it.
func TestGenerationRetentionRunnerForWiresRelationshipSweeper(t *testing.T) {
	cfg := generationRetentionConfig{
		Enabled: true,
		Runner:  loadGenerationRetentionConfig(func(string) string { return "" }).Runner,
	}
	require.Nil(t, generationRetentionRunnerFor(&fakeReducerDB{}, cfg, nil, nil).RelationshipSweeper)
	require.NotNil(t, generationRetentionRunnerFor(&fakeReducerBeginnerDB{}, cfg, nil, nil).RelationshipSweeper)
}

// relationshipSweepFakeDB answers the census (and, with orphans, the sweep
// statements) without Postgres and records every statement and transaction, so
// the adapter tests can prove what a pass sends.
type relationshipSweepFakeDB struct {
	orphans    []string
	begun      int
	statements []string
}

func (d *relationshipSweepFakeDB) Begin(context.Context) (db.Transaction, error) {
	d.begun++
	return &relationshipSweepFakeTx{db: d}, nil
}

type relationshipSweepFakeTx struct{ db *relationshipSweepFakeDB }

func (tx *relationshipSweepFakeTx) record(query string) {
	tx.db.statements = append(tx.db.statements, strings.SplitN(query, "\n", 2)[0])
}

func (tx *relationshipSweepFakeTx) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	tx.record(query)
	switch {
	case strings.Contains(query, "relationship retention: census evidence"):
		rows := make([][]any, 0, len(tx.db.orphans))
		for _, id := range tx.db.orphans {
			rows = append(rows, []any{id})
		}
		return &orphanSweepFakeRows{rows: rows}, nil
	case strings.Contains(query, "relationship retention: census"):
		return &orphanSweepFakeRows{}, nil
	case strings.Contains(query, "relationship retention: try-lock generations"):
		rows := make([][]any, 0, len(tx.db.orphans))
		for _, id := range tx.db.orphans {
			rows = append(rows, []any{id, true})
		}
		return &orphanSweepFakeRows{rows: rows}, nil
	case strings.Contains(query, "relationship retention: classify and lock generations"):
		rows := make([][]any, 0, len(tx.db.orphans))
		for _, id := range tx.db.orphans {
			rows = append(rows, []any{id, true, true})
		}
		return &orphanSweepFakeRows{rows: rows}, nil
	}
	return nil, fmt.Errorf("unexpected query: %s", query)
}

func (tx *relationshipSweepFakeTx) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	tx.record(query)
	if strings.Contains(query, "relationship retention: delete") {
		return relationshipSweepFakeResult(7), nil
	}
	return relationshipSweepFakeResult(0), nil
}

func (*relationshipSweepFakeTx) Commit() error   { return nil }
func (*relationshipSweepFakeTx) Rollback() error { return nil }

type relationshipSweepFakeResult int64

func (relationshipSweepFakeResult) LastInsertId() (int64, error)   { return 0, nil }
func (r relationshipSweepFakeResult) RowsAffected() (int64, error) { return int64(r), nil }

// TestRelationshipRetentionSweeperTakesTheCensusAndDeletesNothingWhenOff is
// the rollout pin at the adapter: with deletes off the pass opens exactly one
// transaction (the read-only census), sends no DELETE, and reports the orphan
// generations per table.
func TestRelationshipRetentionSweeperTakesTheCensusAndDeletesNothingWhenOff(t *testing.T) {
	database := &relationshipSweepFakeDB{orphans: []string{"gen-a", "gen-b", "gen-c"}}
	sweeper := relationshipRetentionSweeper{database: database}
	result, err := sweeper.SweepRelationships(context.Background(), retention.RelationshipSweepRequest{
		Deadline: 15 * time.Minute, RowCap: 100_000,
	})
	require.NoError(t, err)
	require.Equal(t, 1, database.begun, "a census-only pass opens the census transaction and no batch")
	for _, statement := range database.statements {
		require.NotContains(t, statement, "delete", "deletes are off: no DELETE statement may be sent")
	}
	require.Equal(t, int64(3), result.OrphanGenerations[relationshipretention.EvidenceTable])
	require.Equal(t, int64(0), result.OrphanGenerations[relationshipretention.CandidatesTable])
	require.Equal(t, int64(0), result.OrphanGenerations[relationshipretention.GenerationsTable])
	require.False(t, result.CensusSaturated)
	require.Empty(t, result.Capped)
	require.Zero(t, result.GenerationsSwept)
	require.Zero(t, result.Batches)
}

// TestRelationshipRetentionSweeperSweepsWhenEnabled proves the opt-in reaches
// the leaf: with deletes on the census list is swept in committed batches and
// the leaf's per-table rows, generations and batches land on the result.
func TestRelationshipRetentionSweeperSweepsWhenEnabled(t *testing.T) {
	database := &relationshipSweepFakeDB{orphans: []string{"gen-a", "gen-b"}}
	sweeper := relationshipRetentionSweeper{database: database}
	result, err := sweeper.SweepRelationships(context.Background(), retention.RelationshipSweepRequest{
		Deadline: 15 * time.Minute, RowCap: 100_000, Deletes: true,
	})
	require.NoError(t, err)
	require.Equal(t, 2, database.begun, "the census transaction and one batch transaction")
	require.Equal(t, int64(7), result.RowsSwept[relationshipretention.EvidenceTable])
	require.Equal(t, int64(7), result.RowsSwept[relationshipretention.CandidatesTable])
	require.Equal(t, int64(7), result.RowsSwept[relationshipretention.GenerationsTable])
	require.Equal(t, 2, result.GenerationsSwept)
	require.Equal(t, 1, result.Batches)
	require.Empty(t, result.Capped)
	require.Positive(t, result.SweepDuration)
}

// TestRelationshipRetentionSweeperHandsTheRowCapToTheLeaf pins the cap
// wiring: a cap below one batch's rows stops the pass after that batch with
// Capped=rows, so the runner can shorten its next wait.
func TestRelationshipRetentionSweeperHandsTheRowCapToTheLeaf(t *testing.T) {
	orphans := make([]string, 0, relationshipretention.BatchGenerations+1)
	for i := 0; i <= relationshipretention.BatchGenerations; i++ {
		orphans = append(orphans, fmt.Sprintf("gen-%03d", i))
	}
	database := &relationshipSweepFakeDB{orphans: orphans}
	result, err := relationshipRetentionSweeper{database: database}.SweepRelationships(context.Background(), retention.RelationshipSweepRequest{
		RowCap: 10, Deletes: true,
	})
	require.NoError(t, err)
	require.Equal(t, relationshipretention.CapRows, result.Capped)
	require.Equal(t, 1, result.Batches, "the first batch crosses the cap, the second never starts")
}

// TestRelationshipRetentionSweeperHandsTheDeadlineToTheLeaf pins the wall
// budget wiring: a deadline that has passed by the time the first batch commits
// stops the pass before a second one, with Capped=deadline (the first batch
// always runs), and a request without a deadline sweeps every batch.
func TestRelationshipRetentionSweeperHandsTheDeadlineToTheLeaf(t *testing.T) {
	orphans := make([]string, 0, relationshipretention.BatchGenerations+1)
	for i := 0; i <= relationshipretention.BatchGenerations; i++ {
		orphans = append(orphans, fmt.Sprintf("gen-%03d", i))
	}
	for _, tc := range []struct {
		name        string
		deadline    time.Duration
		wantCapped  string
		wantBatches int
	}{
		{"a deadline that passes after the first batch", time.Nanosecond, relationshipretention.CapDeadline, 1},
		{"no deadline", 0, "", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := relationshipRetentionSweeper{database: &relationshipSweepFakeDB{orphans: orphans}}.SweepRelationships(
				context.Background(), retention.RelationshipSweepRequest{Deadline: tc.deadline, Deletes: true})
			require.NoError(t, err)
			require.Equal(t, tc.wantCapped, result.Capped)
			require.Equal(t, tc.wantBatches, result.Batches)
		})
	}
}

// TestRelationshipRetentionSweeperKeysMatchTheRunner pins the strings the
// runner reads off the adapter's result: the reducer retention package owns the
// lock-timeout and cap spellings that decide the accelerated re-run, so a
// rename in the storage leaf must fail here and not silently stop the guard.
func TestRelationshipRetentionSweeperKeysMatchTheRunner(t *testing.T) {
	require.Equal(t, retention.RelationshipSkipLockTimeout, relationshipretention.SkipLockTimeout)
}
