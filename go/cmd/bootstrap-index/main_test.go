// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestPrintBootstrapIndexVersionFlagReturnsBeforeBootstrapWorkflow(t *testing.T) {
	original := buildinfo.Version
	buildinfo.Version = "v1.2.3-bootstrap"
	t.Cleanup(func() { buildinfo.Version = original })

	var stdout strings.Builder
	handled, err := printBootstrapIndexVersionFlag([]string{"--version"}, &stdout)
	if err != nil {
		t.Fatalf("printBootstrapIndexVersionFlag() error = %v, want nil", err)
	}
	if !handled {
		t.Fatal("printBootstrapIndexVersionFlag() handled = false, want true")
	}
	if got, want := stdout.String(), "eshu-bootstrap-index v1.2.3-bootstrap\n"; got != want {
		t.Fatalf("printBootstrapIndexVersionFlag() output = %q, want %q", got, want)
	}
}

func TestRunAppliesSchemaAndDrainsCollectorAndProjector(t *testing.T) {
	t.Parallel()

	db := &fakeBootstrapDB{}
	schemaApplied := false
	contentIndexesFinalized := false
	committer := &fakeCommitter{}

	err := run(
		context.Background(),
		func(string) string { return "" },
		func(context.Context, func(string) string) (bootstrapDB, error) {
			return db, nil
		},
		func(ctx context.Context, database bootstrapDB) error {
			schemaApplied = true
			return nil
		},
		func(context.Context, bootstrapDB) error {
			if got := committer.snapshotCalls(); len(got) == 0 || got[len(got)-1] != "enqueue_drift" {
				t.Fatalf("content index finalizer ran before bootstrap pipeline completed: calls=%v", got)
			}
			contentIndexesFinalized = true
			return nil
		},
		func(context.Context, bootstrapDB, func(string) string, *slog.Logger) error {
			return nil
		},
		func(context.Context, bootstrapDB, func(string) string, trace.Tracer, *telemetry.Instruments) (graphDeps, error) {
			return graphDeps{writer: &noopCanonicalWriter{}, close: func() error { return nil }}, nil
		},
		func(ctx context.Context, database bootstrapDB, getenv func(string) string, _ trace.Tracer, _ *telemetry.Instruments, _ *slog.Logger) (collectorDeps, error) {
			return collectorDeps{
				source: &fakeSource{
					generations: []collector.CollectedGeneration{
						{
							Scope:              scope.IngestionScope{ScopeID: "s1"},
							EstimatedFactCount: 0,
						},
					},
				},
				committer: committer,
			}, nil
		},
		func(ctx context.Context, database bootstrapDB, graphWriter projector.CanonicalWriter, getenv func(string) string, _ trace.Tracer, _ *telemetry.Instruments, _ *slog.Logger) (projectorDeps, error) {
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
	if !schemaApplied {
		t.Fatal("run() did not apply schema")
	}
	if !contentIndexesFinalized {
		t.Fatal("run() did not finalize content substring indexes after the pipeline")
	}
	if !db.closed {
		t.Fatal("run() did not close database")
	}
}

func TestRunReturnsSchemaError(t *testing.T) {
	t.Parallel()

	db := &fakeBootstrapDB{}
	schemaErr := errors.New("schema failed")

	err := run(
		context.Background(),
		func(string) string { return "" },
		func(context.Context, func(string) string) (bootstrapDB, error) {
			return db, nil
		},
		func(ctx context.Context, database bootstrapDB) error {
			return schemaErr
		},
		func(context.Context, bootstrapDB) error {
			t.Fatal("content index finalizer should not run after schema error")
			return nil
		},
		func(context.Context, bootstrapDB, func(string) string, *slog.Logger) error {
			t.Fatal("graph schema check should not run after postgres schema error")
			return nil
		},
		func(context.Context, bootstrapDB, func(string) string, trace.Tracer, *telemetry.Instruments) (graphDeps, error) {
			t.Fatal("graph opener should not be called after schema error")
			return graphDeps{}, nil
		},
		func(ctx context.Context, database bootstrapDB, getenv func(string) string, _ trace.Tracer, _ *telemetry.Instruments, _ *slog.Logger) (collectorDeps, error) {
			t.Fatal("collector builder should not be called after schema error")
			return collectorDeps{}, nil
		},
		func(ctx context.Context, database bootstrapDB, graphWriter projector.CanonicalWriter, getenv func(string) string, _ trace.Tracer, _ *telemetry.Instruments, _ *slog.Logger) (projectorDeps, error) {
			t.Fatal("projector builder should not be called after schema error")
			return projectorDeps{}, nil
		},
	)
	if !errors.Is(err, schemaErr) {
		t.Fatalf("run() error = %v, want %v", err, schemaErr)
	}
	if !db.closed {
		t.Fatal("run() did not close database")
	}
}

func TestRunReturnsCollectorError(t *testing.T) {
	t.Parallel()

	db := &fakeBootstrapDB{}
	collectorErr := errors.New("collector build failed")

	err := run(
		context.Background(),
		func(string) string { return "" },
		func(context.Context, func(string) string) (bootstrapDB, error) {
			return db, nil
		},
		func(ctx context.Context, database bootstrapDB) error {
			return nil
		},
		func(context.Context, bootstrapDB) error {
			t.Fatal("content index finalizer should not run after collector build error")
			return nil
		},
		func(context.Context, bootstrapDB, func(string) string, *slog.Logger) error {
			return nil
		},
		func(context.Context, bootstrapDB, func(string) string, trace.Tracer, *telemetry.Instruments) (graphDeps, error) {
			return graphDeps{writer: &noopCanonicalWriter{}, close: func() error { return nil }}, nil
		},
		func(ctx context.Context, database bootstrapDB, getenv func(string) string, _ trace.Tracer, _ *telemetry.Instruments, _ *slog.Logger) (collectorDeps, error) {
			return collectorDeps{}, collectorErr
		},
		func(ctx context.Context, database bootstrapDB, graphWriter projector.CanonicalWriter, getenv func(string) string, _ trace.Tracer, _ *telemetry.Instruments, _ *slog.Logger) (projectorDeps, error) {
			t.Fatal("projector builder should not be called after collector error")
			return projectorDeps{}, nil
		},
	)
	if !errors.Is(err, collectorErr) {
		t.Fatalf("run() error = %v, want %v", err, collectorErr)
	}
	if !db.closed {
		t.Fatal("run() did not close database")
	}
}

func TestBuildBootstrapCollectorUsesNativeSnapshotter(t *testing.T) {
	t.Parallel()

	deps, err := buildBootstrapCollector(
		context.Background(),
		&fakeBootstrapSQLDB{},
		func(string) string { return "" },
		nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("buildBootstrapCollector() error = %v, want nil", err)
	}

	source, ok := deps.source.(*collector.GitSource)
	if !ok {
		t.Fatalf("buildBootstrapCollector() source type = %T, want *collector.GitSource", deps.source)
	}
	if _, ok := source.Selector.(collector.NativeRepositorySelector); !ok {
		t.Fatalf("buildBootstrapCollector() selector type = %T, want collector.NativeRepositorySelector", source.Selector)
	}
	if _, ok := source.Snapshotter.(collector.NativeRepositorySnapshotter); !ok {
		t.Fatalf("buildBootstrapCollector() snapshotter type = %T, want collector.NativeRepositorySnapshotter", source.Snapshotter)
	}
}

func TestBuildBootstrapCollectorWiresDiscoveryPathGlobOverlay(t *testing.T) {
	t.Parallel()

	deps, err := buildBootstrapCollector(
		context.Background(),
		&fakeBootstrapSQLDB{},
		func(key string) string {
			if key == "ESHU_DISCOVERY_IGNORED_PATH_GLOBS" {
				return "generated/**=generated-template"
			}
			return ""
		},
		nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("buildBootstrapCollector() error = %v, want nil", err)
	}

	source := deps.source.(*collector.GitSource)
	snapshotter := source.Snapshotter.(collector.NativeRepositorySnapshotter)
	if got, want := len(snapshotter.DiscoveryOptions.IgnoredPathGlobs), 1; got != want {
		t.Fatalf("IgnoredPathGlobs length = %d, want %d", got, want)
	}
}

func TestBuildBootstrapProjectorWiresPhasePublisherAndRepairQueue(t *testing.T) {
	t.Parallel()

	deps, err := buildBootstrapProjector(
		context.Background(),
		&fakeBootstrapSQLDB{},
		&noopCanonicalWriter{},
		func(string) string { return "" },
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("buildBootstrapProjector() error = %v, want nil", err)
	}

	runtime, ok := deps.runner.(projector.Runtime)
	if !ok {
		t.Fatalf("buildBootstrapProjector() runner type = %T, want projector.Runtime", deps.runner)
	}
	if runtime.PhasePublisher == nil {
		t.Fatal("buildBootstrapProjector() PhasePublisher = nil, want non-nil")
	}
	if runtime.RepairQueue == nil {
		t.Fatal("buildBootstrapProjector() RepairQueue = nil, want non-nil")
	}
	if runtime.PackageRegistryIdentityLocker == nil {
		t.Fatal("buildBootstrapProjector() PackageRegistryIdentityLocker = nil, want non-nil")
	}
}

func TestBuildBootstrapProjectorClaimsOnlyGitScopes(t *testing.T) {
	t.Parallel()

	deps, err := buildBootstrapProjector(
		context.Background(),
		&fakeBootstrapSQLDB{},
		&noopCanonicalWriter{},
		func(string) string { return "" },
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("buildBootstrapProjector() error = %v, want nil", err)
	}

	queue, ok := deps.workSource.(postgres.ProjectorQueue)
	if !ok {
		t.Fatalf("buildBootstrapProjector() workSource type = %T, want postgres.ProjectorQueue", deps.workSource)
	}
	if got, want := queue.ClaimSourceSystem, string(scope.CollectorGit); got != want {
		t.Fatalf("ClaimSourceSystem = %q, want %q", got, want)
	}
}
