// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestRunEnsuresGraphSchemaBeforeOpeningGraph(t *testing.T) {
	t.Parallel()

	db := &fakeBootstrapDB{}
	schemaApplied := false
	graphSchemaEnsured := false
	graphOpened := false

	err := run(
		context.Background(),
		func(string) string { return "" },
		func(context.Context, func(string) string) (bootstrapDB, error) {
			return db, nil
		},
		func(context.Context, bootstrapDB) error {
			schemaApplied = true
			return nil
		},
		func(context.Context, bootstrapDB) error { return nil },
		func(context.Context, bootstrapDB, func(string) string, *slog.Logger) error {
			if !schemaApplied {
				t.Fatal("graph schema check ran before postgres schema")
			}
			graphSchemaEnsured = true
			return nil
		},
		func(context.Context, bootstrapDB, func(string) string, trace.Tracer, *telemetry.Instruments) (graphDeps, error) {
			if !graphSchemaEnsured {
				t.Fatal("graph opened before graph schema was ensured")
			}
			graphOpened = true
			return graphDeps{writer: &noopCanonicalWriter{}, close: func() error { return nil }}, nil
		},
		func(context.Context, bootstrapDB, func(string) string, trace.Tracer, *telemetry.Instruments, *slog.Logger) (collectorDeps, error) {
			return collectorDeps{
				source: &fakeSource{
					generations: []collector.CollectedGeneration{
						{
							Scope:              scope.IngestionScope{ScopeID: "s1"},
							EstimatedFactCount: 0,
						},
					},
				},
				committer: &fakeCommitter{},
			}, nil
		},
		func(context.Context, bootstrapDB, projector.CanonicalWriter, func(string) string, trace.Tracer, *telemetry.Instruments, *slog.Logger) (projectorDeps, error) {
			return projectorDeps{
				workSource: &fakeWorkSource{
					items: []projector.ScopeGenerationWork{
						{Scope: scope.IngestionScope{ScopeID: "s1"}},
					},
				},
				factStore: &fakeFactStore{},
				runner:    &fakeProjectionRunner{},
				workSink:  &fakeWorkSink{},
			}, nil
		},
	)
	if err != nil {
		t.Fatalf("run() error = %v, want nil", err)
	}
	if !graphOpened {
		t.Fatal("run() did not open graph")
	}
}

func TestRunReturnsGraphSchemaErrorBeforeOpeningGraph(t *testing.T) {
	t.Parallel()

	db := &fakeBootstrapDB{}
	graphSchemaErr := errors.New("graph schema failed")

	err := run(
		context.Background(),
		func(string) string { return "" },
		func(context.Context, func(string) string) (bootstrapDB, error) {
			return db, nil
		},
		func(context.Context, bootstrapDB) error {
			return nil
		},
		func(context.Context, bootstrapDB) error {
			t.Fatal("content index finalizer should not run after graph schema error")
			return nil
		},
		func(context.Context, bootstrapDB, func(string) string, *slog.Logger) error {
			return graphSchemaErr
		},
		func(context.Context, bootstrapDB, func(string) string, trace.Tracer, *telemetry.Instruments) (graphDeps, error) {
			t.Fatal("graph opener should not be called after graph schema error")
			return graphDeps{}, nil
		},
		func(context.Context, bootstrapDB, func(string) string, trace.Tracer, *telemetry.Instruments, *slog.Logger) (collectorDeps, error) {
			t.Fatal("collector builder should not be called after graph schema error")
			return collectorDeps{}, nil
		},
		func(context.Context, bootstrapDB, projector.CanonicalWriter, func(string) string, trace.Tracer, *telemetry.Instruments, *slog.Logger) (projectorDeps, error) {
			t.Fatal("projector builder should not be called after graph schema error")
			return projectorDeps{}, nil
		},
	)
	if !errors.Is(err, graphSchemaErr) {
		t.Fatalf("run() error = %v, want %v", err, graphSchemaErr)
	}
}

func TestEnsureBootstrapGraphSchemaAppliesAndMarksMissingMarker(t *testing.T) {
	t.Parallel()

	app := graph.MustSchemaApplicationForBackend(graph.SchemaBackendNornicDB)
	db := &graphSchemaBootstrapDB{app: app, missingUntilMarked: true}
	executor := &recordingGraphSchemaExecutor{}
	closed := false
	err := ensureBootstrapGraphSchemaWithOpener(
		context.Background(),
		db,
		func(string) string { return "" },
		nil,
		func(context.Context, func(string) string) (graph.SchemaBackend, graph.CypherExecutor, func() error, error) {
			return graph.SchemaBackendNornicDB, executor, func() error {
				closed = true
				return nil
			}, nil
		},
	)
	if err != nil {
		t.Fatalf("ensureBootstrapGraphSchemaWithOpener() error = %v, want nil", err)
	}
	if executor.count == 0 {
		t.Fatal("graph schema executor was not called")
	}
	if !db.marked {
		t.Fatal("graph schema marker was not written")
	}
	if !closed {
		t.Fatal("graph schema executor was not closed")
	}
}

func TestEnsureBootstrapGraphSchemaRejectsIncompatibleMarkerWithoutOpeningGraph(t *testing.T) {
	t.Parallel()

	db := &graphSchemaBootstrapDB{
		app: graph.MustSchemaApplicationForBackend(graph.SchemaBackendNornicDB),
		rows: [][]any{{
			"different-fingerprint",
			[]byte(`[]`),
		}},
	}
	opened := false
	err := ensureBootstrapGraphSchemaWithOpener(
		context.Background(),
		db,
		func(string) string { return "" },
		nil,
		func(context.Context, func(string) string) (graph.SchemaBackend, graph.CypherExecutor, func() error, error) {
			opened = true
			return "", nil, nil, nil
		},
	)
	if err == nil {
		t.Fatal("ensureBootstrapGraphSchemaWithOpener() error = nil, want incompatible marker error")
	}
	if opened {
		t.Fatal("graph schema opener called for incompatible marker")
	}
	if db.marked {
		t.Fatal("graph schema marker was written for incompatible marker")
	}
}

func TestEnsureBootstrapGraphSchemaDoesNotMarkAfterApplyFailure(t *testing.T) {
	t.Parallel()

	app := graph.MustSchemaApplicationForBackend(graph.SchemaBackendNornicDB)
	db := &graphSchemaBootstrapDB{app: app, missingUntilMarked: true}
	executor := &recordingGraphSchemaExecutor{err: errors.New("schema apply failed")}
	err := ensureBootstrapGraphSchemaWithOpener(
		context.Background(),
		db,
		func(string) string { return "" },
		nil,
		func(context.Context, func(string) string) (graph.SchemaBackend, graph.CypherExecutor, func() error, error) {
			return graph.SchemaBackendNornicDB, executor, func() error { return nil }, nil
		},
	)
	if err == nil {
		t.Fatal("ensureBootstrapGraphSchemaWithOpener() error = nil, want apply error")
	}
	if db.marked {
		t.Fatal("graph schema marker was written after apply failure")
	}
}

type graphSchemaBootstrapDB struct {
	app                graph.SchemaApplication
	missingUntilMarked bool
	rows               [][]any
	marked             bool
	markArgs           []any
}

func (f *graphSchemaBootstrapDB) Close() error {
	return nil
}

func (f *graphSchemaBootstrapDB) ExecContext(_ context.Context, _ string, args ...any) (sql.Result, error) {
	f.marked = true
	f.markArgs = args
	return nil, nil
}

func (f *graphSchemaBootstrapDB) QueryContext(context.Context, string, ...any) (postgres.Rows, error) {
	if f.marked {
		return &fakeBootstrapRows{
			rows: [][]any{{f.app.Fingerprint, []byte(`[]`)}},
		}, nil
	}
	if f.missingUntilMarked {
		return &fakeBootstrapRows{}, nil
	}
	return &fakeBootstrapRows{rows: f.rows}, nil
}

type recordingGraphSchemaExecutor struct {
	count int
	err   error
}

func (r *recordingGraphSchemaExecutor) ExecuteCypher(context.Context, graph.CypherStatement) error {
	r.count++
	return r.err
}
