// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	indexretention "github.com/eshu-hq/eshu/go/internal/storage/postgres/search/index/retention"
)

// TestLoadGenerationRetentionSupersededSweepStaysOffByDefault pins the #8231
// enablement posture: an unset flag leaves the sweep off, and only an
// explicit true opts in.
func TestLoadGenerationRetentionSupersededSweepStaysOffByDefault(t *testing.T) {
	cfg := loadGenerationRetentionConfig(func(string) string { return "" })
	require.False(t, cfg.Runner.Policy.SearchIndexSupersededSweepEnabled)

	on := loadGenerationRetentionConfig(func(key string) string {
		if key == generationRetentionSearchIndexSupersededSweepEnabledEnv {
			return "true"
		}
		return ""
	})
	require.True(t, on.Runner.Policy.SearchIndexSupersededSweepEnabled)
}

// TestGenerationRetentionRunnerForWiresSupersededSweeper pins the #8231
// wiring: a database that can begin batch transactions gets the search index
// superseded sweeper alongside the orphan sweeper; one that cannot keeps the
// runner without either.
func TestGenerationRetentionRunnerForWiresSupersededSweeper(t *testing.T) {
	cfg := generationRetentionConfig{
		Enabled: true,
		Runner:  loadGenerationRetentionConfig(func(string) string { return "" }).Runner,
	}
	require.Nil(t, generationRetentionRunnerFor(&fakeReducerDB{}, cfg, nil).SupersededSweeper)
	wired := generationRetentionRunnerFor(&fakeReducerBeginnerDB{}, cfg, nil)
	require.NotNil(t, wired.SupersededSweeper)
	require.NotNil(t, wired.OrphanSweeper)
}

// supersededSweepFakeDB answers the candidate read with one superseded
// generation and the terms walk with a short page then an empty from-start
// re-sweep, so the adapter test runs the sweep without Postgres.
type supersededSweepFakeDB struct{ termsCalls int }

func (d *supersededSweepFakeDB) Begin(context.Context) (db.Transaction, error) {
	return &supersededSweepFakeTx{db: d}, nil
}

func (*supersededSweepFakeDB) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	if strings.Contains(query, "superseded sweep candidates") {
		return &orphanSweepFakeRows{rows: [][]any{{"scope-a", "gen-old"}}}, nil
	}
	return nil, fmt.Errorf("unexpected statement: %s", query)
}

type supersededSweepFakeTx struct{ db *supersededSweepFakeDB }

func (tx *supersededSweepFakeTx) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	switch {
	case strings.Contains(query, "pg_try_advisory_xact_lock"):
		return &orphanSweepFakeRows{rows: [][]any{{true}}}, nil
	case strings.Contains(query, "superseded terms sweep batch"):
		tx.db.termsCalls++
		if tx.db.termsCalls == 1 {
			return &orphanSweepFakeRows{rows: [][]any{{true, int64(5), int64(5), "k9", "d9"}}}, nil
		}
		return &orphanSweepFakeRows{rows: [][]any{{true, int64(0), int64(0), "", ""}}}, nil
	case strings.Contains(query, "superseded documents sweep batch"):
		return &orphanSweepFakeRows{rows: [][]any{{true, int64(0), int64(0), "", ""}}}, nil
	case strings.Contains(query, "superseded stats sweep batch"):
		return &orphanSweepFakeRows{rows: [][]any{{true, int64(1)}}}, nil
	}
	return nil, fmt.Errorf("unexpected statement: %s", query)
}

func (*supersededSweepFakeTx) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, nil
}
func (*supersededSweepFakeTx) Commit() error   { return nil }
func (*supersededSweepFakeTx) Rollback() error { return nil }

// TestSearchIndexSupersededSweeperSweepsListedCandidates proves the reducer
// wiring hands the runner's cap and deadline to the sweep and maps its
// result: the listed generation's terms, documents, and stats rows land on
// the result by table.
func TestSearchIndexSupersededSweeperSweepsListedCandidates(t *testing.T) {
	sweeper := searchIndexSupersededSweeper{database: &supersededSweepFakeDB{}}
	result, err := sweeper.SweepSearchIndexSuperseded(context.Background(), 0, 0)
	require.NoError(t, err)
	require.Equal(t, 1, result.Candidates)
	// Terms: one short page then the empty from-start re-sweep; documents:
	// one empty page; stats: the single row.
	require.Equal(t, int64(5), result.RowsSwept[indexretention.TermsTable])
	require.Equal(t, int64(0), result.RowsSwept[indexretention.DocumentsTable])
	require.Equal(t, int64(1), result.RowsSwept[indexretention.StatsTable])
	require.Equal(t, 0, result.Unswept)
}
