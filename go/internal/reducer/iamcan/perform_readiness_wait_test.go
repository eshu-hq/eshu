// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iamcan

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// memoryWaitLedger is an in-memory crossscope.ReadinessWaitLedger with the
// store's earliest-anchor rule.
type memoryWaitLedger struct {
	mu      sync.Mutex
	rows    map[string]crossscope.ReadinessWait
	upserts int
	clears  int
}

func newMemoryWaitLedger() *memoryWaitLedger {
	return &memoryWaitLedger{rows: make(map[string]crossscope.ReadinessWait)}
}

func (l *memoryWaitLedger) GetReadinessWait(_ context.Context, scopeID string, domain reducercontract.Domain) (crossscope.ReadinessWait, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	row, ok := l.rows[scopeID+"|"+string(domain)]
	return row, ok, nil
}

func (l *memoryWaitLedger) UpsertReadinessWait(_ context.Context, wait crossscope.ReadinessWait) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.upserts++
	key := wait.ScopeID + "|" + string(wait.Domain)
	if existing, ok := l.rows[key]; ok {
		// The store's anchor_epoch fence: drop a lower epoch, keep the
		// earlier anchor at an equal epoch.
		if wait.AnchorEpoch < existing.AnchorEpoch {
			return nil
		}
		if wait.AnchorEpoch == existing.AnchorEpoch && existing.FirstDeferredAt.Before(wait.FirstDeferredAt) {
			wait.FirstDeferredAt = existing.FirstDeferredAt
		}
		wait.RowVersion = existing.RowVersion + 1
	} else {
		wait.RowVersion = 0
	}
	wait.ClearedAt = time.Time{}
	l.rows[key] = wait
	return nil
}

func (l *memoryWaitLedger) ClearReadinessWait(_ context.Context, wait crossscope.ReadinessWait) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.clears++
	key := wait.ScopeID + "|" + string(wait.Domain)
	existing, ok := l.rows[key]
	// The store's clear fence: the read epoch and row version must both
	// still match.
	if !ok || existing.AnchorEpoch != wait.AnchorEpoch || existing.RowVersion != wait.RowVersion {
		return nil
	}
	l.rows[key] = crossscope.ReadinessWait{
		ScopeID: wait.ScopeID, Domain: wait.Domain, FirstDeferredAt: wait.ClearedAt,
		AnchorEpoch: existing.AnchorEpoch + 1, RowVersion: existing.RowVersion + 1,
		CommittedGenerationID:   wait.CommittedGenerationID,
		CommittedCycleStartedAt: wait.CommittedCycleStartedAt, ClearedAt: wait.ClearedAt, UpdatedAt: wait.ClearedAt,
	}
	return nil
}

func (l *memoryWaitLedger) row(t *testing.T) (crossscope.ReadinessWait, bool) {
	t.Helper()
	row, ok, _ := l.GetReadinessWait(context.Background(), crossScopeIAMScope, reducercontract.DomainIAMCanPerformMaterialization)
	return row, ok
}

// waitHandler grants one action on each of two buckets, in separate
// statements so each resolves as its own exact-ARN edge. waitSnapshot decides
// which bucket is ready.
func waitHandler(ledger crossscope.ReadinessWaitLedger, loader CrossScopeTargetLoader, clock *time.Time) (IAMCanPerformMaterializationHandler, *stubFactLoader) {
	factLoader := &stubFactLoader{envelopes: []facts.Envelope{
		crossScopeRoleFact(),
		crossScopePermission([]string{"s3:putobject"}, []string{crossScopeBucket}),
		crossScopePermission([]string{"s3:getobject"}, []string{crossScopeBucketB}),
	}}
	return IAMCanPerformMaterializationHandler{
		FactLoader:        factLoader,
		Writer:            &recordingIAMCanPerformWriter{},
		CrossScopeTargets: loader,
		ReadinessWaits:    ledger,
		Now:               func() time.Time { return *clock },
	}, factLoader
}

const crossScopeS3WestScope = "aws:123456789012:us-west-2:s3"

// waitSnapshot puts crossScopeBucket in a committed s3 scope and
// crossScopeBucketB in a second s3 scope whose nodes have not committed
// (not_ready), or committed when bReady.
func waitSnapshot(bReady bool) CrossScopeTargetSnapshot {
	return CrossScopeTargetSnapshot{
		Scopes: []CrossScopeTargetScope{
			{ScopeID: crossScopeS3Scope, ActiveGenerationID: "s3-gen-1", GenerationActive: true, NodesCommitted: true},
			{ScopeID: crossScopeS3WestScope, ActiveGenerationID: "s3w-gen-1", GenerationActive: true, NodesCommitted: bReady},
		},
		Resources: map[string][]facts.Envelope{
			crossScopeS3Scope:     {crossScopeBucketFact(crossScopeBucket)},
			crossScopeS3WestScope: {crossScopeBucketFact(crossScopeBucketB)},
		},
	}
}

func waitIntent(generation string, cycle time.Time) reducercontract.Intent {
	intent := crossScopeIntent()
	intent.GenerationID = generation
	intent.CycleStartedAt = cycle
	intent.EnqueuedAt = cycle
	return intent
}

func requireTargetNotReady(t *testing.T, err error) {
	t.Helper()
	var classified interface {
		Retryable() bool
		FailureClass() string
	}
	if err == nil || !errors.As(err, &classified) || !classified.Retryable() ||
		classified.FailureClass() != IAMCanPerformTargetNotReadyFailureClass {
		t.Fatalf("Handle() error = %v, want the retryable %s defer", err, IAMCanPerformTargetNotReadyFailureClass)
	}
}

// TestIAMCanPerformCommitsReadyEdgesBeforeDeferring is the commit-first
// contract: the ready bucket's edge is written at the first evaluation even
// though another target's scope has not committed its nodes, and only then does the intent
// defer. A poll with the same missing target writes and retracts nothing and
// skips the fact load; a poll after the target resolves re-commits once and
// clears the ledger.
func TestIAMCanPerformCommitsReadyEdgesBeforeDeferring(t *testing.T) {
	t.Parallel()
	clock := waitTestNow()
	ledger := newMemoryWaitLedger()
	loader := &fakeCrossScopeTargets{snapshot: waitSnapshot(false)}
	handler, factLoader := waitHandler(ledger, loader, &clock)
	intent := waitIntent("iam-gen-1", clock)

	_, err := handler.Handle(context.Background(), intent)
	requireTargetNotReady(t, err)
	first := handler.Writer.(*recordingIAMCanPerformWriter)
	if first.edgeCalls != 1 || len(first.edgeRows) != 1 || first.retractCalls != 1 {
		t.Fatalf("first evaluation: edge calls %d rows %d retracts %d, want the ready edge committed (1/1/1)",
			first.edgeCalls, len(first.edgeRows), first.retractCalls)
	}
	row, ok := ledger.row(t)
	if !ok || row.MissingCount != 1 || row.MissingKeys[0] != crossScopeBucketB || row.CommittedGenerationID != "iam-gen-1" {
		t.Fatalf("ledger row = %+v (found %v), want missing [%s] committed at iam-gen-1", row, ok, crossScopeBucketB)
	}

	clock = clock.Add(30 * time.Second)
	poll := &recordingIAMCanPerformWriter{}
	handler.Writer = poll
	loadsBefore := factLoader.calls
	_, err = handler.Handle(context.Background(), intent)
	requireTargetNotReady(t, err)
	if poll.edgeCalls != 0 || poll.retractCalls != 0 {
		t.Fatalf("unchanged poll touched the graph: edges %d retracts %d, want 0/0", poll.edgeCalls, poll.retractCalls)
	}
	if factLoader.calls != loadsBefore {
		t.Fatalf("unchanged poll loaded facts (%d calls), want the cheap poll path", factLoader.calls-loadsBefore)
	}
	if got := loader.requests[len(loader.requests)-1].Targets; len(got) != 1 || got[0].ARN != crossScopeBucketB {
		t.Fatalf("poll asked for %+v, want only the missing target", got)
	}

	clock = clock.Add(30 * time.Second)
	loader.snapshot = waitSnapshot(true)
	resolved := &recordingIAMCanPerformWriter{}
	handler.Writer = resolved
	if _, err := handler.Handle(context.Background(), intent); err != nil {
		t.Fatalf("Handle() after the target resolved error = %v, want success", err)
	}
	if resolved.edgeCalls != 1 || len(resolved.edgeRows) != 2 || resolved.retractCalls != 1 {
		t.Fatalf("resolve re-commit: edge calls %d rows %d retracts %d, want exactly one re-commit of 2 edges",
			resolved.edgeCalls, len(resolved.edgeRows), resolved.retractCalls)
	}
	if row, ok := ledger.row(t); !ok || !row.Cleared() || row.MissingCount != 0 {
		t.Fatalf("ledger row after the missing set emptied = %+v (found %v), want a cleared tombstone", row, ok)
	}
}

// stubRefreshGraph replays canned gate rows (or an error) for the value-flow
// refresh emit gate.
type stubRefreshGraph struct {
	rows []map[string]any
	err  error
}

func (s *stubRefreshGraph) Run(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
	return s.rows, s.err
}

// TestIAMCanPerformReportsAffectedRepos pins the #6785 emit gate: a
// productive run reports the affected-repo count for the refresh ACK.
func TestIAMCanPerformReportsAffectedRepos(t *testing.T) {
	t.Parallel()
	clock := waitTestNow()
	handler, _ := waitHandler(newMemoryWaitLedger(), &fakeCrossScopeTargets{snapshot: waitSnapshot(true)}, &clock)
	handler.AffectedGraph = &stubRefreshGraph{rows: []map[string]any{{"repo_id": "r1"}}}
	result, err := handler.Handle(context.Background(), waitIntent("iam-gen-1", clock))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.CanonicalWrites == 0 {
		t.Fatal("CanonicalWrites = 0, want the committed edges")
	}
	if got := result.SubSignals["refresh_affected_repos"]; got != 1 {
		t.Errorf("refresh_affected_repos = %v, want 1", got)
	}
}

// TestIAMCanPerformGateErrorFailsOpen pins fail-open: a gate read error must
// not suppress the refresh.
func TestIAMCanPerformGateErrorFailsOpen(t *testing.T) {
	t.Parallel()
	clock := waitTestNow()
	handler, _ := waitHandler(newMemoryWaitLedger(), &fakeCrossScopeTargets{snapshot: waitSnapshot(true)}, &clock)
	handler.AffectedGraph = &stubRefreshGraph{err: errors.New("graph down")}
	result, err := handler.Handle(context.Background(), waitIntent("iam-gen-1", clock))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if got := result.SubSignals["refresh_affected_repos"]; got != 1 {
		t.Errorf("refresh_affected_repos = %v, want 1 (fail open)", got)
	}
}

// TestIAMCanPerformRevokedGrantRetractsAtFirstEvaluation is §4.5 item 2: gen
// N+1 drops the grant on the ready bucket while another target stays missing.
// The scope-wide retract runs at N+1's first evaluation, so the revoked edge
// does not stay readable while the wait continues.
func TestIAMCanPerformRevokedGrantRetractsAtFirstEvaluation(t *testing.T) {
	t.Parallel()
	clock := waitTestNow()
	ledger := newMemoryWaitLedger()
	loader := &fakeCrossScopeTargets{snapshot: waitSnapshot(false)}
	handler, factLoader := waitHandler(ledger, loader, &clock)
	_, err := handler.Handle(context.Background(), waitIntent("iam-gen-1", clock))
	requireTargetNotReady(t, err)

	clock = clock.Add(5 * time.Minute)
	factLoader.envelopes = []facts.Envelope{
		crossScopeRoleFact(),
		crossScopePermission([]string{"s3:getobject"}, []string{crossScopeBucketB}),
	}
	next := &recordingIAMCanPerformWriter{}
	handler.Writer = next
	_, err = handler.Handle(context.Background(), waitIntent("iam-gen-2", clock))
	requireTargetNotReady(t, err)
	if next.retractCalls != 1 || len(next.edgeRows) != 0 {
		t.Fatalf("gen-2 first evaluation: retracts %d, rows %d; want the scope-wide retract and no edge", next.retractCalls, len(next.edgeRows))
	}
}

// TestIAMCanPerformSettledMissingSetCommitsWithoutDeferring is §4.5 item 3 and
// the R2-F1 closing shape: the wait's anchor survives a superseding
// generation, the bound settles the missing set once (abandoned), and a later
// generation with the same missing set succeeds at its first claim without
// counting another defer.
func TestIAMCanPerformSettledMissingSetCommitsWithoutDeferring(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	clock := waitTestNow()
	start := clock
	ledger := newMemoryWaitLedger()
	loader := &fakeCrossScopeTargets{snapshot: waitSnapshot(false)}
	handler, _ := waitHandler(ledger, loader, &clock)
	handler.Instruments = instruments
	handler.ReadinessMaxWait = 10 * time.Minute

	_, err = handler.Handle(context.Background(), waitIntent("iam-gen-1", clock))
	requireTargetNotReady(t, err)

	clock = start.Add(6 * time.Minute) // gen-2 supersedes gen-1 before the bound
	_, err = handler.Handle(context.Background(), waitIntent("iam-gen-2", clock))
	requireTargetNotReady(t, err)

	clock = start.Add(10 * time.Minute) // anchor + MaxWait, measured from gen-1
	gen2 := &recordingIAMCanPerformWriter{}
	handler.Writer = gen2
	if _, err := handler.Handle(context.Background(), waitIntent("iam-gen-2", start.Add(6*time.Minute))); err != nil {
		t.Fatalf("Handle() at anchor+MaxWait error = %v, want the wait to settle", err)
	}
	if gen2.edgeCalls != 0 || gen2.retractCalls != 0 {
		t.Fatalf("settling poll re-committed: edges %d retracts %d, want 0 (gen-2 already committed)", gen2.edgeCalls, gen2.retractCalls)
	}
	deferredBefore := readinessWaitSum(t, reader, crossscope.ReadinessWaitDeferred)
	if got := readinessWaitSum(t, reader, crossscope.ReadinessWaitAbandoned); got != 1 {
		t.Fatalf("abandoned = %d, want exactly 1", got)
	}

	clock = start.Add(40 * time.Minute)
	gen3 := &recordingIAMCanPerformWriter{}
	handler.Writer = gen3
	if _, err := handler.Handle(context.Background(), waitIntent("iam-gen-3", clock)); err != nil {
		t.Fatalf("gen-3 first claim error = %v, want success on the settled missing set", err)
	}
	if gen3.edgeCalls != 1 || len(gen3.edgeRows) != 1 {
		t.Fatalf("gen-3 commit: edge calls %d rows %d, want the ready edge", gen3.edgeCalls, len(gen3.edgeRows))
	}
	if got := readinessWaitSum(t, reader, crossscope.ReadinessWaitDeferred); got != deferredBefore {
		t.Fatalf("deferred moved from %d to %d on a settled set, want unchanged", deferredBefore, got)
	}
	if got := readinessWaitSum(t, reader, crossscope.ReadinessWaitSettledMissing); got != 1 {
		t.Fatalf("settled_missing = %d, want 1", got)
	}
}

// TestIAMCanPerformCrossScopeOutcomesOnlyOnCommit proves the per-target
// counter is emitted by evaluations that commit, not by every poll (review
// P3-C), so not_ready no longer scales with the retry count.
func TestIAMCanPerformCrossScopeOutcomesOnlyOnCommit(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	clock := waitTestNow()
	handler, _ := waitHandler(newMemoryWaitLedger(), &fakeCrossScopeTargets{snapshot: waitSnapshot(false)}, &clock)
	handler.Instruments = instruments
	intent := waitIntent("iam-gen-1", clock)
	for i := 0; i < 5; i++ {
		_, _ = handler.Handle(context.Background(), intent)
		clock = clock.Add(30 * time.Second)
	}
	if got := metricSum(t, reader, "eshu_dp_iam_can_perform_cross_scope_targets_total", crossScopeTargetNotReady); got != 1 {
		t.Fatalf("not_ready = %d after 1 commit and 4 polls, want 1", got)
	}
}

func waitTestNow() time.Time { return time.Date(2026, time.September, 19, 9, 0, 0, 0, time.UTC) }

func readinessWaitSum(t *testing.T, reader *sdkmetric.ManualReader, outcome string) int64 {
	t.Helper()
	return metricSum(t, reader, "eshu_dp_reducer_readiness_waits_total", outcome)
}

// metricSum totals one counter's data points whose outcome attribute matches.
func metricSum(t *testing.T, reader *sdkmetric.ManualReader, name, outcome string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	var total int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			if m.Name != name || !ok {
				continue
			}
			for _, point := range sum.DataPoints {
				if value, ok := point.Attributes.Value("outcome"); ok && value.AsString() == outcome {
					total += point.Value
				}
			}
		}
	}
	return total
}
