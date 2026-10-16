// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph"
)

// slowGraphRunner delays every statement and can hang on one stage, so a test
// can make the whole gate outlast one statement timeout while each statement
// stays inside it, or make one stage never return.
type slowGraphRunner struct {
	inner  *fakeGraphRunner
	delay  time.Duration
	hangOn func(cypher string) bool
}

func (r *slowGraphRunner) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	if r.hangOn != nil && r.hangOn(cypher) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	select {
	case <-time.After(r.delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return r.inner.Run(ctx, cypher, params)
}

// deadlineBootstrapDB honors the context deadline on writes, as database/sql
// does, so a marker write under an expired context fails the way it would in
// production. fakeBootstrapDB ignores its context.
type deadlineBootstrapDB struct{ *fakeBootstrapDB }

func (d deadlineBootstrapDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return d.fakeBootstrapDB.ExecContext(ctx, query, args...)
}

// runNeo4jAdoptionWithTimeout runs Neo4j adoption with a complete catalog and
// the given graph statement timeout.
func runNeo4jAdoptionWithTimeout(t *testing.T, runner *slowGraphRunner, timeout string) (*fakeBootstrapDB, string, error) {
	t.Helper()
	expected, err := expectedGraphSchemaObjectNames(graph.SchemaBackendNeo4j)
	if err != nil {
		t.Fatal(err)
	}
	db := &fakeBootstrapDB{queryRows: []fakeBootstrapRows{{rows: nil}}}
	var logs bytes.Buffer
	err = run(
		context.Background(),
		func(key string) string {
			switch key {
			case "ESHU_GRAPH_BACKEND":
				return "neo4j"
			case graphSchemaAdoptExistingEnv:
				return "true"
			case graphSchemaStatementTimeoutEnv:
				return timeout
			}
			return ""
		},
		slog.New(slog.NewJSONHandler(&logs, nil)),
		func(context.Context, func(string) string) (bootstrapDB, error) { return deadlineBootstrapDB{db}, nil },
		func(context.Context, bootstrapExecutor) error { return nil },
		func(context.Context, func(string) string) (neo4jDeps, error) {
			return neo4jDeps{
				executor:  &fakeNeo4jExecutor{},
				inspector: &fakeGraphSchemaInspector{names: expected},
				runner:    runner,
				close:     func() error { return nil },
			}, nil
		},
		func(context.Context, graph.CypherExecutor, *slog.Logger, graph.SchemaBackend) error { return nil },
	)
	return db, logs.String(), err
}

// TestAdoptionGateMayOutlastOneStatementTimeout pins F5: the adoption
// deadline bounds the catalog inspection only. A gate of 30 statements at
// 40 ms each takes about 1.2 s, longer than the 500 ms statement timeout,
// while every statement stays inside it; adoption must still write its
// marker instead of failing on the inspection deadline.
func TestAdoptionGateMayOutlastOneStatementTimeout(t *testing.T) {
	t.Parallel()

	var events []string
	runner := &slowGraphRunner{inner: &fakeGraphRunner{events: &events}, delay: 40 * time.Millisecond}
	db, _, err := runNeo4jAdoptionWithTimeout(t, runner, "500ms")
	if err != nil {
		t.Fatalf("run() error = %v; a gate longer than one statement timeout must not fail the marker write", err)
	}
	if !markerWritten(db) {
		t.Fatal("adoption marker not written after a slow but healthy gate")
	}
	if len(events) < 30 {
		t.Fatalf("gate ran %d statements, want the full probe, backfill, wait, and state read", len(events))
	}
}

// TestAdoptionGateThatHangsFailsClosedAndNamesTheCause keeps the fail-closed
// behavior: a gate stage that never returns is cut off by its own statement
// timeout, the marker stays unset, and the error names the stage and the
// timeout variable an operator would raise. A wait hangs because the index
// is not ONLINE yet, so that case's state read reports POPULATING; a wait
// that times out on an index the state read then shows ONLINE passes
// (TestIndexStateDecidesAfterAnAwaitError).
func TestAdoptionGateThatHangsFailsClosedAndNamesTheCause(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		stage  string
		hangOn func(string) bool
		state  string
	}{
		{"readiness probe", func(c string) bool { return strings.Contains(c, "RETURN count(n) AS oversized") }, ""},
		{"backfill", func(c string) bool { return strings.Contains(c, "IN TRANSACTIONS") }, ""},
		{"index wait", func(c string) bool { return strings.HasPrefix(c, "CALL db.awaitIndex(") }, "POPULATING"},
	} {
		var events []string
		runner := &slowGraphRunner{inner: &fakeGraphRunner{events: &events, indexState: tc.state}, hangOn: tc.hangOn}
		db, _, err := runNeo4jAdoptionWithTimeout(t, runner, "100ms")
		if err == nil {
			t.Fatalf("%s hang: run() error = nil, want a refusal", tc.stage)
		}
		for _, want := range []string{"infra search key " + tc.stage, graphSchemaStatementTimeoutEnv} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s hang: error %q does not name %q", tc.stage, err, want)
			}
		}
		if markerWritten(db) {
			t.Fatalf("%s hang: adoption marker written", tc.stage)
		}
	}
}
