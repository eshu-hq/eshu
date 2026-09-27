// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/trace"
)

// lockedRun drives run() with a recording secret-lines lifecycle so a test can
// assert where the bulk-load lock sits in the run and that it is released on
// every path (#7125, P2-D1).
type lockedRun struct {
	mu           sync.Mutex
	events       []string
	db           *fakeBootstrapDB
	lockErr      error
	releaseErr   error
	schemaErr    error
	collectErr   error
	finalizeErr  error
	waitSeen     time.Duration
	releasedOpen bool
}

func (r *lockedRun) record(event string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *lockedRun) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.events)
}

func (r *lockedRun) execute() error {
	r.db = &fakeBootstrapDB{}
	committer := &fakeCommitter{}
	secretLines := secretLinesLifecycle{
		lock: func(_ context.Context, _ bootstrapDB, _ *slog.Logger, wait time.Duration) (func(context.Context) error, error) {
			r.record("lock")
			r.waitSeen = wait
			if r.lockErr != nil {
				return nil, r.lockErr
			}
			return func(context.Context) error {
				r.record("release")
				r.releasedOpen = !r.db.closed
				return r.releaseErr
			}, nil
		},
		begin: func(context.Context, bootstrapDB) (int64, error) {
			r.record("begin")
			return 7, nil
		},
		finalize: func(context.Context, bootstrapDB, int64, *slog.Logger, *telemetry.Instruments) error {
			r.record("finalize")
			return r.finalizeErr
		},
	}
	return run(
		context.Background(),
		func(key string) string {
			if key == "ESHU_SCHEMA_BOOTSTRAP_OWNERSHIP_WAIT" {
				return "45s"
			}
			return ""
		},
		func(context.Context, func(string) string) (bootstrapDB, error) { return r.db, nil },
		func(context.Context, bootstrapDB, *slog.Logger) error {
			r.record("schema")
			return r.schemaErr
		},
		func(context.Context, bootstrapDB) error { return nil },
		secretLines,
		func(context.Context, bootstrapDB, func(string) string, *slog.Logger) error { return nil },
		func(context.Context, bootstrapDB, func(string) string, trace.Tracer, *telemetry.Instruments) (graphDeps, error) {
			return graphDeps{writer: &noopCanonicalWriter{}, close: func() error { return nil }}, nil
		},
		func(context.Context, bootstrapDB, func(string) string, trace.Tracer, *telemetry.Instruments, *slog.Logger) (collectorDeps, error) {
			r.record("collector_build")
			if r.collectErr != nil {
				return collectorDeps{}, r.collectErr
			}
			return collectorDeps{
				source: &fakeSource{generations: []collector.CollectedGeneration{
					{Scope: scope.IngestionScope{ScopeID: "s1"}},
				}},
				committer: committer,
			}, nil
		},
		func(context.Context, bootstrapDB, runtime.CanonicalWriter, func(string) string, trace.Tracer, *telemetry.Instruments, *slog.Logger) (projectorDeps, error) {
			return projectorDeps{
				workSource: &fakeWorkSource{items: []projector.ScopeGenerationWork{{Scope: scope.IngestionScope{ScopeID: "s1"}}}},
				factStore:  &fakeFactStore{},
				runner:     &fakeProjectionRunner{},
				workSink:   &fakeWorkSink{},
			}, nil
		},
	)
}

// TestRunHoldsTheBulkLoadLockFromBeforeTheSchemaUntilAfterFinalize proves the
// run-scoped lock brackets the whole deferred load: taken first (before the
// schema apply and BeginDeferral), released last (after finalize, while the
// database is still open), with the schema ownership wait as its bound.
func TestRunHoldsTheBulkLoadLockFromBeforeTheSchemaUntilAfterFinalize(t *testing.T) {
	t.Parallel()
	r := &lockedRun{}
	if err := r.execute(); err != nil {
		t.Fatalf("run() = %v, want nil", err)
	}
	events := r.snapshot()
	index := func(event string) int {
		t.Helper()
		i := slices.Index(events, event)
		if i < 0 {
			t.Fatalf("event %q never ran; events = %v", event, events)
		}
		return i
	}
	if index("lock") != 0 {
		t.Fatalf("the bulk-load lock must be the first step; events = %v", events)
	}
	order := []string{"lock", "schema", "begin", "collector_build", "finalize", "release"}
	for k := 1; k < len(order); k++ {
		if index(order[k-1]) >= index(order[k]) {
			t.Fatalf("want lock -> schema -> begin -> pipeline -> finalize -> release; events = %v", events)
		}
	}
	if events[len(events)-1] != "release" {
		t.Fatalf("release must be the last step; events = %v", events)
	}
	if !r.releasedOpen {
		t.Fatal("the lock was released after the database closed; its pinned connection must be released first")
	}
	if r.waitSeen != 45*time.Second {
		t.Fatalf("lock wait = %v, want the schema ownership wait (45s)", r.waitSeen)
	}
}

// TestRunReleasesTheBulkLoadLockOnEveryFailurePath proves a failed run never
// leaves the lock held for the process lifetime: schema failure, a failure
// after BeginDeferral, and a finalizer failure all release it.
func TestRunReleasesTheBulkLoadLockOnEveryFailurePath(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	for _, tc := range []struct {
		name  string
		setup func(*lockedRun)
		want  []string
	}{
		{"schema fails", func(r *lockedRun) { r.schemaErr = boom }, []string{"lock", "schema", "release"}},
		{"collector build fails after begin", func(r *lockedRun) { r.collectErr = boom }, []string{"lock", "schema", "begin", "collector_build", "release"}},
		{"finalize fails", func(r *lockedRun) { r.finalizeErr = boom }, []string{"lock", "schema", "begin", "collector_build", "finalize", "release"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := &lockedRun{}
			tc.setup(r)
			err := r.execute()
			if !errors.Is(err, boom) {
				t.Fatalf("run() = %v, want the injected failure", err)
			}
			if got := r.snapshot(); !slices.Equal(got, tc.want) {
				t.Fatalf("events = %v, want %v", got, tc.want)
			}
			if !r.releasedOpen {
				t.Fatal("the lock was released after the database closed")
			}
		})
	}
}

// TestRunRefusedBulkLoadLockRunsNothing proves a second run that cannot get the
// lock fails before it applies the schema or begins a deferral: no statement of
// the deferred load executes.
func TestRunRefusedBulkLoadLockRunsNothing(t *testing.T) {
	t.Parallel()
	refused := errors.New("acquire secret lines bulk load ownership: another bootstrapper (pid=9) held it for more than 45s")
	r := &lockedRun{lockErr: refused}
	err := r.execute()
	if !errors.Is(err, refused) {
		t.Fatalf("run() = %v, want the refusal", err)
	}
	if got := r.snapshot(); !slices.Equal(got, []string{"lock"}) {
		t.Fatalf("events = %v, want only the refused lock", got)
	}
	if !r.db.closed {
		t.Fatal("the database was not closed after the refusal")
	}
}

// TestRunJoinsAReleaseFailureIntoTheRunError proves a void-exclusivity release
// fails an otherwise successful run instead of being swallowed.
func TestRunJoinsAReleaseFailureIntoTheRunError(t *testing.T) {
	t.Parallel()
	voided := errors.New("release secret lines bulk load lock: backend 4 no longer held it")
	r := &lockedRun{releaseErr: voided}
	if err := r.execute(); !errors.Is(err, voided) {
		t.Fatalf("run() = %v, want the release failure joined in", err)
	}
	other := errors.New("finalize failed")
	r = &lockedRun{releaseErr: voided, finalizeErr: other}
	err := r.execute()
	if !errors.Is(err, voided) || !errors.Is(err, other) {
		t.Fatalf("run() = %v, want both the finalize and the release failures", err)
	}
}

// TestProductionSecretLinesLockNeedsAPinnableConnection proves the production
// lock refuses a bootstrap database that cannot pin a connection, instead of
// running the deferred load unguarded.
func TestProductionSecretLinesLockNeedsAPinnableConnection(t *testing.T) {
	t.Parallel()
	_, err := productionSecretLines().lock(context.Background(), &fakeBootstrapDB{}, slog.New(slog.DiscardHandler), time.Second)
	if err == nil {
		t.Fatal("lock on a database without Conn = nil error, want a refusal")
	}
}
