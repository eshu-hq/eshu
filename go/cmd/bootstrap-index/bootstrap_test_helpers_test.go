// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// --- fakes ---

type fakeBootstrapDB struct {
	closed bool
}

func (f *fakeBootstrapDB) Close() error {
	f.closed = true
	return nil
}

func (f *fakeBootstrapDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, nil
}

func (f *fakeBootstrapDB) QueryContext(context.Context, string, ...any) (postgres.Rows, error) {
	app := graph.MustSchemaApplicationForBackend(graph.SchemaBackendNornicDB)
	return &fakeBootstrapRows{
		rows: [][]any{{app.Fingerprint, []byte(`[]`)}},
	}, nil
}

type fakeBootstrapSQLDB = fakeBootstrapDB

type fakeBootstrapRows struct {
	rows  [][]any
	index int
}

func (r *fakeBootstrapRows) Next() bool {
	return r.index < len(r.rows)
}

func (r *fakeBootstrapRows) Scan(dest ...any) error {
	if r.index >= len(r.rows) {
		return errors.New("scan called without row")
	}
	row := r.rows[r.index]
	if len(dest) != len(row) {
		return fmt.Errorf("scan destination count = %d, want %d", len(dest), len(row))
	}
	for i := range dest {
		switch target := dest[i].(type) {
		case *string:
			value, ok := row[i].(string)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want string", i, row[i])
			}
			*target = value
		case *[]byte:
			value, ok := row[i].([]byte)
			if !ok {
				return fmt.Errorf("row[%d] type = %T, want []byte", i, row[i])
			}
			*target = value
		default:
			return fmt.Errorf("scan dest[%d] type = %T", i, dest[i])
		}
	}
	r.index++
	return nil
}

func (r *fakeBootstrapRows) Err() error {
	return nil
}

func (r *fakeBootstrapRows) Close() error {
	return nil
}

type fakeSource struct {
	generations []collector.CollectedGeneration
	index       int
}

func (f *fakeSource) Next(context.Context) (collector.CollectedGeneration, bool, error) {
	if f.index >= len(f.generations) {
		return collector.CollectedGeneration{}, false, nil
	}
	gen := f.generations[f.index]
	f.index++
	return gen, true, nil
}

type fakeCommitter struct {
	mu                sync.Mutex
	calls             []string
	backfillCalls     int
	iacCalls          int
	reopenCalls       int
	reopenedDomains   []string
	driftEnqueueCalls int
	backfillErr       error
	iacErr            error
	reopenErr         error
	driftEnqueueErr   error
	// backfillDelay, when set, makes BackfillAllRelationshipEvidence block for
	// the given duration. Used to prove the projection phase duration excludes
	// the backfill wait (#3678 P2#1).
	backfillDelay time.Duration
	// backfillStarted and backfillRelease, when both non-nil, make
	// BackfillAllRelationshipEvidence close backfillStarted the instant it is
	// entered and then block until backfillRelease is closed, before
	// backfillDelay (if any) and before returning. This lets a test observe
	// state (e.g. captured logs) at the exact moment the call is in flight and
	// has not yet returned, which backfillDelay alone cannot prove: a fixed
	// sleep only bounds how long the call blocks, it does not let the test
	// synchronize with "the call has been entered and is still blocked"
	// (#4271 review follow-up).
	backfillStarted chan struct{}
	backfillRelease chan struct{}
}

func (f *fakeCommitter) CommitScopeGeneration(
	_ context.Context,
	_ scope.IngestionScope,
	_ scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
) error {
	if factStream != nil {
		for range factStream {
		}
	}
	return nil
}

func (f *fakeCommitter) BackfillAllRelationshipEvidence(
	_ context.Context,
	_ trace.Tracer,
	_ *telemetry.Instruments,
) error {
	if f.backfillStarted != nil && f.backfillRelease != nil {
		close(f.backfillStarted)
		<-f.backfillRelease
	}
	if f.backfillDelay > 0 {
		time.Sleep(f.backfillDelay)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "backfill")
	f.backfillCalls++
	return f.backfillErr
}

func (f *fakeCommitter) MaterializeIaCReachability(
	_ context.Context,
	_ trace.Tracer,
	_ *telemetry.Instruments,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "iac_reachability")
	f.iacCalls++
	return f.iacErr
}

func (f *fakeCommitter) ReopenDeploymentMappingWorkItems(
	_ context.Context,
	_ trace.Tracer,
	_ *telemetry.Instruments,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "reopen")
	f.reopenCalls++
	return f.reopenErr
}

func (f *fakeCommitter) ReopenCodeImportRepoEdgeWorkItems(
	_ context.Context,
	_ trace.Tracer,
	_ *telemetry.Instruments,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "reopen_code_import")
	return nil
}

func (f *fakeCommitter) ReopenSucceededReducerWorkItems(
	_ context.Context,
	_ trace.Tracer,
	_ *telemetry.Instruments,
	domains []string,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "reopen_correlation")
	f.reopenedDomains = append([]string(nil), domains...)
	return nil
}

func (f *fakeCommitter) EnqueueConfigStateDriftIntents(
	_ context.Context,
	_ trace.Tracer,
	_ *telemetry.Instruments,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "enqueue_drift")
	f.driftEnqueueCalls++
	return f.driftEnqueueErr
}

func (f *fakeCommitter) snapshotCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

type fakeWorkSource struct {
	mu    sync.Mutex
	items []projector.ScopeGenerationWork
	index int
}

func (f *fakeWorkSource) Claim(context.Context) (projector.ScopeGenerationWork, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.index >= len(f.items) {
		return projector.ScopeGenerationWork{}, false, nil
	}
	item := f.items[f.index]
	f.index++
	return item, true, nil
}

type fakeFactStore struct{}

func (f *fakeFactStore) LoadFacts(context.Context, projector.ScopeGenerationWork) ([]facts.Envelope, error) {
	return nil, nil
}

type fakeProjectionRunner struct{}

func (f *fakeProjectionRunner) Project(
	context.Context,
	scope.IngestionScope,
	scope.ScopeGeneration,
	[]facts.Envelope,
) (projector.Result, error) {
	return projector.Result{}, nil
}

type fakeWorkSink struct{}

func (f *fakeWorkSink) Ack(context.Context, projector.ScopeGenerationWork, projector.Result) error {
	return nil
}

func (f *fakeWorkSink) Fail(context.Context, projector.ScopeGenerationWork, error) error {
	return nil
}

// testFactChannel creates a pre-filled closed channel for testing.
func testFactChannel(envelopes ...facts.Envelope) <-chan facts.Envelope {
	ch := make(chan facts.Envelope, len(envelopes))
	for _, e := range envelopes {
		ch <- e
	}
	close(ch)
	return ch
}

// --- thread-safe fakes for concurrency tests ---

type concurrentWorkSource struct {
	mu    sync.Mutex
	items []projector.ScopeGenerationWork
	index int
}

func (f *concurrentWorkSource) Claim(context.Context) (projector.ScopeGenerationWork, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.index >= len(f.items) {
		return projector.ScopeGenerationWork{}, false, nil
	}
	item := f.items[f.index]
	f.index++
	return item, true, nil
}

type concurrentWorkSink struct {
	acked atomic.Int64
}

func (f *concurrentWorkSink) Ack(context.Context, projector.ScopeGenerationWork, projector.Result) error {
	f.acked.Add(1)
	return nil
}

func (f *concurrentWorkSink) Fail(context.Context, projector.ScopeGenerationWork, error) error {
	return nil
}

// --- additional fakes for pipelined tests ---

type slowSource struct {
	mu          sync.Mutex
	generations []collector.CollectedGeneration
	index       int
	delay       time.Duration
	finished    time.Time
}

func (f *slowSource) Next(ctx context.Context) (collector.CollectedGeneration, bool, error) {
	f.mu.Lock()
	if f.index >= len(f.generations) {
		if f.finished.IsZero() {
			f.finished = time.Now()
		}
		f.mu.Unlock()
		return collector.CollectedGeneration{}, false, nil
	}
	gen := f.generations[f.index]
	f.index++
	f.mu.Unlock()

	select {
	case <-ctx.Done():
		return collector.CollectedGeneration{}, false, ctx.Err()
	case <-time.After(f.delay):
	}

	return gen, true, nil
}

func (f *slowSource) collectionFinishedTime() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.finished
}

type failingSource struct {
	err error
}

func (f *failingSource) Next(context.Context) (collector.CollectedGeneration, bool, error) {
	return collector.CollectedGeneration{}, false, f.err
}

type projectionTracker struct {
	mu    sync.Mutex
	first time.Time
}

func (p *projectionTracker) Project(
	_ context.Context,
	_ scope.IngestionScope,
	_ scope.ScopeGeneration,
	_ []facts.Envelope,
) (projector.Result, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.first.IsZero() {
		p.first = time.Now()
	}
	return projector.Result{}, nil
}

func (p *projectionTracker) firstProjectionTime() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.first
}

type failingProjectionRunner struct {
	mu        sync.Mutex
	count     int
	failAfter int
	err       error
}

func (f *failingProjectionRunner) Project(
	_ context.Context,
	_ scope.IngestionScope,
	_ scope.ScopeGeneration,
	_ []facts.Envelope,
) (projector.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.count++
	if f.count > f.failAfter {
		return projector.Result{}, f.err
	}
	return projector.Result{}, nil
}

type delayedProjectionRunner struct {
	delay time.Duration
}

func (d *delayedProjectionRunner) Project(
	ctx context.Context,
	_ scope.IngestionScope,
	_ scope.ScopeGeneration,
	_ []facts.Envelope,
) (projector.Result, error) {
	select {
	case <-ctx.Done():
		return projector.Result{}, ctx.Err()
	case <-time.After(d.delay):
	}
	return projector.Result{}, nil
}

type blockingProjectionRunner struct {
	started chan<- struct{}
	release <-chan struct{}
	once    sync.Once
}

func (r *blockingProjectionRunner) Project(
	ctx context.Context,
	_ scope.IngestionScope,
	_ scope.ScopeGeneration,
	_ []facts.Envelope,
) (projector.Result, error) {
	r.once.Do(func() { close(r.started) })
	select {
	case <-ctx.Done():
		return projector.Result{}, ctx.Err()
	case <-r.release:
		return projector.Result{}, nil
	}
}

type recordingProjectorHeartbeater struct {
	countValue atomic.Int64
}

func (r *recordingProjectorHeartbeater) Heartbeat(context.Context, projector.ScopeGenerationWork) error {
	r.countValue.Add(1)
	return nil
}

func (r *recordingProjectorHeartbeater) count() int64 {
	return r.countValue.Load()
}

func (r *recordingProjectorHeartbeater) waitForHeartbeats(want int64, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if r.count() >= want {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return r.count() >= want
}

type contextCanceledProjectorHeartbeater struct {
	entered chan struct{}
	once    sync.Once
}

func (c *contextCanceledProjectorHeartbeater) Heartbeat(ctx context.Context, _ projector.ScopeGenerationWork) error {
	c.once.Do(func() { close(c.entered) })
	<-ctx.Done()
	return ctx.Err()
}

type noopCanonicalWriter struct{}

func (*noopCanonicalWriter) Write(_ context.Context, _ projector.CanonicalMaterialization) error {
	return nil
}
