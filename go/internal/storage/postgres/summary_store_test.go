// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser/summary"
)

func TestFunctionSummarySchemaSQL(t *testing.T) {
	t.Parallel()

	sql := FunctionSummarySchemaSQL()
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS function_summaries",
		"function_id TEXT PRIMARY KEY",
		"effects JSONB NOT NULL",
		"version TEXT NOT NULL",
		"structural_hash TEXT NOT NULL",
		"repo TEXT NOT NULL DEFAULT ''",
		"updated_at TIMESTAMPTZ NOT NULL",
		"function_summaries_repo_idx",
		"function_summaries_updated_idx",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("schema missing %q:\n%s", want, sql)
		}
	}
}

func TestBootstrapDefinitionsIncludeFunctionSummaries(t *testing.T) {
	t.Parallel()

	var found Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "function_summaries" {
			found = def
			break
		}
	}
	if found.Name == "" {
		t.Fatal("BootstrapDefinitions missing function_summaries")
	}
	if found.Path != "schema/data-plane/postgres/028_function_summaries.sql" {
		t.Fatalf("Path = %q", found.Path)
	}
	if !strings.Contains(found.SQL, "CREATE TABLE IF NOT EXISTS function_summaries") {
		t.Fatalf("definition SQL missing function_summaries table:\n%s", found.SQL)
	}
}

func TestFunctionSummaryStoreUpsertsSnapshot(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 18, 4, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{}
	store := NewFunctionSummaryStore(db)
	snap := summarySnapshotFixture()

	if err := store.UpsertSnapshot(context.Background(), snap, now); err != nil {
		t.Fatalf("UpsertSnapshot() error = %v", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	exec := db.execs[0]
	for _, want := range []string{
		"INSERT INTO function_summaries",
		"ON CONFLICT (function_id) DO UPDATE",
		"effects = EXCLUDED.effects",
		"version = EXCLUDED.version",
		"structural_hash = EXCLUDED.structural_hash",
		"updated_at = EXCLUDED.updated_at",
		"WHERE function_summaries.updated_at <= EXCLUDED.updated_at",
	} {
		if !strings.Contains(exec.query, want) {
			t.Fatalf("upsert query missing %q:\n%s", want, exec.query)
		}
	}
	if got, want := len(exec.args), 12; got != want {
		t.Fatalf("arg count = %d, want %d", got, want)
	}
	if exec.args[0] != string(snap.Functions[0].ID) || exec.args[6] != string(snap.Functions[1].ID) {
		t.Fatalf("function id args = %#v, want deterministic snapshot order", exec.args)
	}
}

func TestFunctionSummaryStoreRejectsBlankRepoComponent(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewFunctionSummaryStore(db)
	err := store.UpsertSnapshot(context.Background(), summary.Snapshot{Functions: []summary.SnapshotFunction{{
		ID:      summary.NewFunctionID("", "pkg", "", "handler"),
		Effects: summary.Effects{ParamToReturn: []int{0}},
		Version: "version-1",
	}}}, time.Date(2026, 6, 18, 4, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("UpsertSnapshot() error = nil, want blank repo rejection")
	}
	if !strings.Contains(err.Error(), "repo is required") {
		t.Fatalf("UpsertSnapshot() error = %v, want repo requirement", err)
	}
	if len(db.execs) != 0 {
		t.Fatalf("exec count = %d, want 0", len(db.execs))
	}
}

func TestFunctionSummaryStoreRejectsMalformedFunctionID(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewFunctionSummaryStore(db)
	err := store.UpsertSnapshot(context.Background(), summary.Snapshot{Functions: []summary.SnapshotFunction{{
		ID:      summary.FunctionID("handler"),
		Effects: summary.Effects{ParamToReturn: []int{0}},
		Version: "version-1",
	}}}, time.Date(2026, 6, 18, 4, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("UpsertSnapshot() error = nil, want malformed FunctionID rejection")
	}
	if !strings.Contains(err.Error(), "repo is required") {
		t.Fatalf("UpsertSnapshot() error = %v, want repo requirement", err)
	}
	if len(db.execs) != 0 {
		t.Fatalf("exec count = %d, want 0", len(db.execs))
	}
}

func TestFunctionSummaryStoreKeepsSameFunctionNamesAcrossReposDistinct(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewFunctionSummaryStore(db)
	alphaID := summary.NewFunctionID("repo-alpha", "pkg", "", "handler")
	betaID := summary.NewFunctionID("repo-beta", "pkg", "", "handler")
	snap := summary.Snapshot{Functions: []summary.SnapshotFunction{
		{ID: alphaID, Effects: summary.Effects{ParamToReturn: []int{0}}, Version: "version-alpha"},
		{ID: betaID, Effects: summary.Effects{ParamToReturn: []int{0}}, Version: "version-beta"},
	}}

	if err := store.UpsertSnapshot(context.Background(), snap, time.Date(2026, 6, 18, 4, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("UpsertSnapshot() error = %v", err)
	}
	exec := db.execs[0]
	if exec.args[0] == exec.args[6] {
		t.Fatalf("same package/function names collided across repos: %#v", exec.args)
	}
	if exec.args[0] != string(alphaID) || exec.args[6] != string(betaID) {
		t.Fatalf("function id args = %#v, want repo-distinct deterministic ids", exec.args)
	}
}

func TestFunctionSummaryStoreEmptySnapshotDoesNotWrite(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewFunctionSummaryStore(db)
	err := store.UpsertSnapshot(context.Background(), summary.Snapshot{}, time.Date(2026, 6, 18, 4, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("UpsertSnapshot() error = %v, want nil", err)
	}
	if len(db.execs) != 0 {
		t.Fatalf("exec count = %d, want 0", len(db.execs))
	}
}

func TestFunctionSummaryStoreLoadsSnapshotIntoStableStore(t *testing.T) {
	t.Parallel()

	snap := summarySnapshotFixture()
	rows := make([][]any, 0, len(snap.Functions))
	for _, fn := range snap.Functions {
		effects, err := json.Marshal(fn.Effects)
		if err != nil {
			t.Fatalf("marshal effects: %v", err)
		}
		rows = append(rows, []any{string(fn.ID), effects, fn.Version})
	}
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: rows}}}
	store := NewFunctionSummaryStore(db)

	loaded, err := store.LoadSnapshot(context.Background())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	reloaded := summary.Load(loaded)
	if recomputed := reloaded.Upsert(map[summary.FunctionID]summary.Effects{
		snap.Functions[0].ID: snap.Functions[0].Effects,
		snap.Functions[1].ID: snap.Functions[1].Effects,
	}); len(recomputed) != 0 {
		t.Fatalf("unchanged reload recomputed %v, want none", recomputed)
	}
	if got := reloaded.Snapshot(); !equalSummarySnapshots(got, snap) {
		t.Fatalf("loaded snapshot = %#v, want %#v", got, snap)
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	if !strings.Contains(db.queries[0].query, "ORDER BY function_id ASC") {
		t.Fatalf("load query missing deterministic ordering:\n%s", db.queries[0].query)
	}
}

func TestFunctionSummaryStoreLoadRejectsMalformedEffects(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: [][]any{{
		string(summary.NewFunctionID("repo-alpha", "pkg", "", "handler")),
		[]byte(`{"ParamToReturn":[`),
		"version-1",
	}}}}}
	store := NewFunctionSummaryStore(db)

	_, err := store.LoadSnapshot(context.Background())
	if err == nil {
		t.Fatal("LoadSnapshot() error = nil, want malformed effects error")
	}
	if !strings.Contains(err.Error(), "decode function summary effects") {
		t.Fatalf("LoadSnapshot() error = %v, want decode context", err)
	}
}

func summarySnapshotFixture() summary.Snapshot {
	repo := "repo-alpha"
	sourceID := summary.NewFunctionID(repo, "pkg", "", "handler")
	sinkID := summary.NewFunctionID(repo, "pkg", "", "query")
	store := summary.NewStore()
	store.Upsert(map[summary.FunctionID]summary.Effects{
		sourceID: {
			ParamToCallArg: []summary.CallArgFlow{{
				Callee: sinkID,
				Param:  0,
				Arg:    0,
			}},
		},
		sinkID: {
			ParamToSink: []summary.ParamSink{{Param: 0, SinkKind: "sql"}},
		},
	})
	return store.Snapshot()
}

func equalSummarySnapshots(a, b summary.Snapshot) bool {
	ab, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bb, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(ab) == string(bb)
}
