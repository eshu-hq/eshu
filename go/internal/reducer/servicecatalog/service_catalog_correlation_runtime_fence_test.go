// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
)

// runtimeFenceEvents records the order of the reads and writes one Handle pass
// makes, so a test can assert that the runtime readiness check runs before the
// runtime read and both run before any write.
type runtimeFenceEvents struct {
	mu     sync.Mutex
	events []string
}

func (e *runtimeFenceEvents) add(event string) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, event)
}

func (e *runtimeFenceEvents) snapshot() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.events...)
}

// fakeRuntimeProducerReadiness serves per-repository producer states the way
// the Postgres store does: a repository with no producer row at its active
// generation is simply absent from the map.
type fakeRuntimeProducerReadiness struct {
	states  map[string]RuntimeProducerState
	err     error
	events  *runtimeFenceEvents
	calls   int
	repoIDs [][]string
	domains [][]reducercontract.Domain
}

func (f *fakeRuntimeProducerReadiness) RuntimeProducerStates(
	_ context.Context,
	repoIDs []string,
	producerDomains []reducercontract.Domain,
) (map[string]RuntimeProducerState, error) {
	f.calls++
	f.repoIDs = append(f.repoIDs, append([]string(nil), repoIDs...))
	f.domains = append(f.domains, append([]reducercontract.Domain(nil), producerDomains...))
	f.events.add("readiness")
	if f.err != nil {
		return nil, f.err
	}
	out := make(map[string]RuntimeProducerState, len(f.states))
	for repoID, state := range f.states {
		out[repoID] = state
	}
	return out, nil
}

// orderedRuntimeLoader records its call in the shared event log.
type orderedRuntimeLoader struct {
	byRepo map[string][]ServiceRuntimeInstance
	events *runtimeFenceEvents
	calls  int
}

func (l *orderedRuntimeLoader) GetRuntimeInstancesForRepos(
	_ context.Context,
	repoIDs []string,
) (map[string][]ServiceRuntimeInstance, error) {
	l.calls++
	l.events.add("runtime_load")
	out := map[string][]ServiceRuntimeInstance{}
	for _, repoID := range repoIDs {
		if instances := l.byRepo[repoID]; len(instances) > 0 {
			out[repoID] = instances
		}
	}
	return out, nil
}

// orderedCorrelationWriter and orderedMaterializationWriter record writes in
// the shared event log.
type orderedCorrelationWriter struct {
	recordingServiceCatalogCorrelationWriter
	events *runtimeFenceEvents
}

func (w *orderedCorrelationWriter) WriteServiceCatalogCorrelations(
	ctx context.Context,
	write ServiceCatalogCorrelationWrite,
) (ServiceCatalogCorrelationWriteResult, error) {
	w.events.add("correlation_write")
	return w.recordingServiceCatalogCorrelationWriter.WriteServiceCatalogCorrelations(ctx, write)
}

type orderedMaterializationWriter struct {
	recordingServiceMaterializationWriter
	events *runtimeFenceEvents
}

func (w *orderedMaterializationWriter) WriteServiceMaterialization(
	ctx context.Context,
	write ServiceMaterializationWrite,
) (ServiceMaterializationWriteResult, error) {
	w.events.add("materialization_write")
	return w.recordingServiceMaterializationWriter.WriteServiceMaterialization(ctx, write)
}

// runtimeFenceInstance is the WorkloadInstance the in-flight producer is
// about to write for repo-checkout.
var runtimeFenceInstance = ServiceRuntimeInstance{
	PlatformKind: "kubernetes", Environment: "prod",
	WorkloadRef: "workload-instance:checkout:prod", WorkloadName: "checkout", Confidence: 0.9,
}

// runtimeFenceNow is the fixed clock every runtime-fence test uses.
var runtimeFenceNow = time.Date(2026, time.October, 15, 12, 0, 0, 0, time.UTC)

// runtimeFenceIntent is a catalog intent whose cycle started startedAgo
// before runtimeFenceNow. A zero startedAgo leaves the anchor unset.
func runtimeFenceIntent(startedAgo time.Duration) reducercontract.Intent {
	intent := fenceTestIntent()
	if startedAgo > 0 {
		intent.CycleStartedAt = runtimeFenceNow.Add(-startedAgo)
		intent.EnqueuedAt = intent.CycleStartedAt
	}
	return intent
}

type runtimeFenceHarness struct {
	events          *runtimeFenceEvents
	readiness       *fakeRuntimeProducerReadiness
	runtime         *orderedRuntimeLoader
	correlations    *orderedCorrelationWriter
	materialization *orderedMaterializationWriter
	handler         ServiceCatalogCorrelationHandler
}

// newRuntimeFenceHarness wires the handler with the production-shaped
// collaborators: a runtime loader that returns runtimeInstances for
// repo-checkout and a readiness port that reports states.
func newRuntimeFenceHarness(
	states map[string]RuntimeProducerState,
	runtimeInstances []ServiceRuntimeInstance,
) *runtimeFenceHarness {
	events := &runtimeFenceEvents{}
	h := &runtimeFenceHarness{
		events:    events,
		readiness: &fakeRuntimeProducerReadiness{states: states, events: events},
		runtime: &orderedRuntimeLoader{
			byRepo: map[string][]ServiceRuntimeInstance{"repo-checkout": runtimeInstances},
			events: events,
		},
		correlations:    &orderedCorrelationWriter{events: events},
		materialization: &orderedMaterializationWriter{events: events},
	}
	h.handler = ServiceCatalogCorrelationHandler{
		FactLoader:               fenceTestFactLoader(),
		Writer:                   h.correlations,
		MaterializationWriter:    h.materialization,
		RuntimeInstanceLoader:    h.runtime,
		RuntimeProducerReadiness: h.readiness,
		Now:                      func() time.Time { return runtimeFenceNow },
	}
	return h
}

// TestServiceCatalogHandlerDefersWhileRuntimeProducerInFlight is the #7816
// regression. The repository's workload_materialization work item is still
// claimed, so its WorkloadInstance nodes are not in the graph yet and the
// runtime read returns nothing. The handler used to commit the service
// generation with an empty runtime family (Neo4j read digest e3b0c442 before
// the MERGE), which no later event repairs. It must defer with a non-counting
// class and write nothing.
func TestServiceCatalogHandlerDefersWhileRuntimeProducerInFlight(t *testing.T) {
	t.Parallel()

	h := newRuntimeFenceHarness(
		map[string]RuntimeProducerState{"repo-checkout": {Claimed: 1, Total: 1}},
		nil, // the producer has not written the WorkloadInstance yet
	)

	_, err := h.handler.Handle(context.Background(), runtimeFenceIntent(time.Minute))
	if err == nil {
		var runtimeRows int
		for _, write := range h.materialization.writes {
			runtimeRows += len(write.Runtime)
		}
		t.Fatalf("Handle() error = nil, want a runtime readiness deferral; the handler committed %d service write(s) with %d runtime row(s) while workload_materialization was in flight",
			len(h.materialization.writes), runtimeRows)
	}
	var classified interface {
		Retryable() bool
		FailureClass() string
	}
	if !errors.As(err, &classified) {
		t.Fatalf("Handle() error = %v (%T), want a retryable classified deferral", err, err)
	}
	if !classified.Retryable() {
		t.Fatal("runtime readiness deferral must be retryable")
	}
	if got, want := classified.FailureClass(), "service_catalog_correlation_runtime_not_ready"; got != want {
		t.Fatalf("FailureClass() = %q, want %q", got, want)
	}
	if got := h.correlations.calls; got != 0 {
		t.Fatalf("WriteServiceCatalogCorrelations calls = %d, want 0: a deferral writes nothing", got)
	}
	if got := len(h.materialization.writes); got != 0 {
		t.Fatalf("WriteServiceMaterialization calls = %d, want 0 while the producer is in flight", got)
	}
	if got := h.runtime.calls; got != 0 {
		t.Fatalf("runtime loader calls = %d, want 0: the runtime read must not run while the fence is open", got)
	}
}

// TestServiceCatalogHandlerChecksRuntimeReadinessBeforeRuntimeLoad pins the
// order: readiness, then the runtime read, then the writes. Checking after the
// read would let a producer that commits between the read and the check pass
// the fence with an empty read in hand.
func TestServiceCatalogHandlerChecksRuntimeReadinessBeforeRuntimeLoad(t *testing.T) {
	t.Parallel()

	h := newRuntimeFenceHarness(
		map[string]RuntimeProducerState{"repo-checkout": {Total: 1}},
		[]ServiceRuntimeInstance{runtimeFenceInstance},
	)
	if _, err := h.handler.Handle(context.Background(), runtimeFenceIntent(time.Minute)); err != nil {
		t.Fatalf("Handle() error = %v, want nil once the producer is terminal", err)
	}
	want := []string{"readiness", "runtime_load", "correlation_write", "materialization_write"}
	if got := h.events.snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("event order = %v, want %v", got, want)
	}
	if got, want := h.readiness.repoIDs, [][]string{{"repo-checkout"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("readiness repo ids = %v, want %v", got, want)
	}
	if got, want := h.readiness.domains, [][]reducercontract.Domain{{reducercontract.DomainWorkloadMaterialization}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("readiness producer domains = %v, want %v", got, want)
	}
	if len(h.materialization.writes) != 1 || len(h.materialization.writes[0].Runtime) != 1 {
		t.Fatalf("materialization writes = %+v, want one write carrying the producer's runtime row", h.materialization.writes)
	}
}

// TestServiceCatalogHandlerRuntimeFenceReadyStates covers every ready answer:
// a terminal producer, a repository with no producer row at its active
// generation (no workloads: absent must be ready or such repos deadlock), and
// a repository whose live work belongs only to a newer pending generation
// (the store does not report it, so it reads as absent).
func TestServiceCatalogHandlerRuntimeFenceReadyStates(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		states  map[string]RuntimeProducerState
		runtime []ServiceRuntimeInstance
		rows    int
	}{
		"terminal producer":        {states: map[string]RuntimeProducerState{"repo-checkout": {Total: 2}}, runtime: []ServiceRuntimeInstance{runtimeFenceInstance}, rows: 1},
		"no producer row":          {states: nil, runtime: nil, rows: 0},
		"only newer pending items": {states: map[string]RuntimeProducerState{}, runtime: []ServiceRuntimeInstance{runtimeFenceInstance}, rows: 1},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := newRuntimeFenceHarness(tc.states, tc.runtime)
			if _, err := h.handler.Handle(context.Background(), runtimeFenceIntent(time.Minute)); err != nil {
				t.Fatalf("Handle() error = %v, want nil", err)
			}
			if len(h.materialization.writes) != 1 {
				t.Fatalf("materialization writes = %d, want 1", len(h.materialization.writes))
			}
			if got := len(h.materialization.writes[0].Runtime); got != tc.rows {
				t.Fatalf("runtime rows = %d, want %d", got, tc.rows)
			}
		})
	}
}

// TestServiceCatalogHandlerRuntimeFenceProceedsPastElapsedBound proves the
// fence cannot wedge: a producer stuck in flight for longer than
// crossscope.ProducerReadinessMaxWait no longer defers the intent. The bound is
// elapsed time since the cycle anchor, never attempt_count, which the
// non-counting class freezes.
func TestServiceCatalogHandlerRuntimeFenceProceedsPastElapsedBound(t *testing.T) {
	t.Parallel()

	h := newRuntimeFenceHarness(
		map[string]RuntimeProducerState{"repo-checkout": {Retrying: 1, Total: 1}},
		[]ServiceRuntimeInstance{runtimeFenceInstance},
	)
	intent := runtimeFenceIntent(crossscope.ProducerReadinessMaxWait + time.Second)
	intent.AttemptCount = 1
	if _, err := h.handler.Handle(context.Background(), intent); err != nil {
		t.Fatalf("Handle() error = %v, want nil past the elapsed bound", err)
	}
	if len(h.materialization.writes) != 1 || len(h.materialization.writes[0].Runtime) != 1 {
		t.Fatalf("materialization writes = %+v, want the best available runtime evidence committed", h.materialization.writes)
	}

	inside := newRuntimeFenceHarness(
		map[string]RuntimeProducerState{"repo-checkout": {Retrying: 1, Total: 1}},
		nil,
	)
	if _, err := inside.handler.Handle(context.Background(), runtimeFenceIntent(crossscope.ProducerReadinessMaxWait-time.Second)); err == nil {
		t.Fatal("Handle() error = nil one second inside the bound, want a deferral")
	}
}

// TestServiceCatalogHandlerRuntimeFenceDefersWithUnknownAnchor keeps the
// crossscope precedent: a zero cycle anchor means elapsed time is unknown,
// not infinite, so the fence keeps deferring instead of committing early.
func TestServiceCatalogHandlerRuntimeFenceDefersWithUnknownAnchor(t *testing.T) {
	t.Parallel()

	h := newRuntimeFenceHarness(map[string]RuntimeProducerState{"repo-checkout": {Pending: 1, Total: 1}}, nil)
	if _, err := h.handler.Handle(context.Background(), runtimeFenceIntent(0)); err == nil {
		t.Fatal("Handle() error = nil with a zero cycle anchor, want a deferral")
	}
}

// TestServiceCatalogHandlerRuntimeFenceSurfacesLookupError proves a readiness
// store that cannot answer is a real failure, never a readiness miss: the
// error must not carry the non-counting class, and nothing is written.
func TestServiceCatalogHandlerRuntimeFenceSurfacesLookupError(t *testing.T) {
	t.Parallel()

	h := newRuntimeFenceHarness(nil, nil)
	h.readiness.err = errors.New("connection refused")
	_, err := h.handler.Handle(context.Background(), runtimeFenceIntent(time.Minute))
	if err == nil {
		t.Fatal("Handle() error = nil, want the readiness lookup error")
	}
	var classified interface{ FailureClass() string }
	if errors.As(err, &classified) && classified.FailureClass() == RuntimeNotReadyFailureClass {
		t.Fatalf("lookup error classified as %q; a store that cannot answer must not hide in a non-counting class", RuntimeNotReadyFailureClass)
	}
	if h.correlations.calls != 0 || len(h.materialization.writes) != 0 || h.runtime.calls != 0 {
		t.Fatalf("writes/loads after a lookup error: correlations=%d materialization=%d runtime=%d, want 0",
			h.correlations.calls, len(h.materialization.writes), h.runtime.calls)
	}
}

// TestServiceCatalogHandlerRuntimeFenceRunsAfterCorpusFence keeps both fences
// ahead of the runtime read and the corpus fence first: an open corpus fence
// defers with its own class before the runtime readiness store is consulted.
func TestServiceCatalogHandlerRuntimeFenceRunsAfterCorpusFence(t *testing.T) {
	t.Parallel()

	h := newRuntimeFenceHarness(map[string]RuntimeProducerState{"repo-checkout": {Running: 1, Total: 1}}, nil)
	h.handler.DeploymentRelationshipLoader = &corpusFenceStubLoader{complete: false}
	_, err := h.handler.Handle(context.Background(), runtimeFenceIntent(time.Minute))
	var classified interface{ FailureClass() string }
	if !errors.As(err, &classified) || classified.FailureClass() != ServiceCatalogCorrelationResolutionNotReadyFailureClass {
		t.Fatalf("Handle() error = %v, want the corpus-fence deferral first", err)
	}
	if h.readiness.calls != 0 || h.runtime.calls != 0 {
		t.Fatalf("readiness calls = %d runtime calls = %d, want 0 while the corpus fence is open", h.readiness.calls, h.runtime.calls)
	}
}

// TestServiceCatalogHandlerRuntimeFenceIdleWithoutRuntimeLoader keeps the
// fence additive: with no runtime loader there is no runtime read to fence.
func TestServiceCatalogHandlerRuntimeFenceIdleWithoutRuntimeLoader(t *testing.T) {
	t.Parallel()

	h := newRuntimeFenceHarness(map[string]RuntimeProducerState{"repo-checkout": {Claimed: 1, Total: 1}}, nil)
	h.handler.RuntimeInstanceLoader = nil
	if _, err := h.handler.Handle(context.Background(), runtimeFenceIntent(time.Minute)); err != nil {
		t.Fatalf("Handle() error = %v, want nil with no runtime loader", err)
	}
	if h.readiness.calls != 0 {
		t.Fatalf("readiness calls = %d, want 0 with no runtime loader", h.readiness.calls)
	}
}
