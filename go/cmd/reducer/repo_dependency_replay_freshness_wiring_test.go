// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// replayFreshnessWiringDB answers only the generation-freshness read: the
// scope's active generation is "gen-active" and the queried generation is a
// superseded, older one.
type replayFreshnessWiringDB struct{ freshnessQueries int }

func (d *replayFreshnessWiringDB) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	if !strings.Contains(query, "FROM ingestion_scopes AS scope") {
		return nil, sql.ErrNoRows
	}
	d.freshnessQueries++
	return &replayFreshnessWiringRows{}, nil
}

func (*replayFreshnessWiringDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, sql.ErrConnDone
}

type replayFreshnessWiringRows struct{ read bool }

func (r *replayFreshnessWiringRows) Next() bool {
	if r.read {
		return false
	}
	r.read = true
	return true
}

func (*replayFreshnessWiringRows) Scan(dest ...any) error {
	*dest[0].(*sql.NullString) = sql.NullString{String: "gen-active", Valid: true}
	*dest[1].(*sql.NullString) = sql.NullString{String: "superseded", Valid: true}
	*dest[2].(*bool) = false
	return nil
}

func (*replayFreshnessWiringRows) Err() error   { return nil }
func (*replayFreshnessWiringRows) Close() error { return nil }

// TestNewRepoDependencyProjectionRunnerWiresGenerationFreshness guards the
// composition root for #7670: without the freshness seam the runner replays
// every accepted generation, and one retired by recover-generations
// quarantines the lane every cycle.
func TestNewRepoDependencyProjectionRunnerWiresGenerationFreshness(t *testing.T) {
	t.Parallel()

	database := &replayFreshnessWiringDB{}
	runner := newRepoDependencyProjectionRunner(
		postgres.NewSharedIntentStore(database), database, nil, postgres.ReducerQueue{},
		nil, nil, nil, reducer.RepoDependencyProjectionRunnerConfig{}, nil, nil, nil,
	)
	if runner.GenerationFreshness == nil {
		t.Fatal("RepoDependencyProjectionRunner.GenerationFreshness is nil, want the Postgres freshness check")
	}
	current, err := runner.GenerationFreshness(context.Background(), "scope-1", "gen-retired")
	if err != nil {
		t.Fatalf("GenerationFreshness() error = %v, want nil", err)
	}
	if current {
		t.Fatal("GenerationFreshness(gen-retired) = true, want false for a superseded generation")
	}
	if database.freshnessQueries != 1 {
		t.Fatalf("freshness queries = %d, want 1", database.freshnessQueries)
	}
}
