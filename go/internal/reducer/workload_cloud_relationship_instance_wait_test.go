// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/reducer/workloadinstance"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// usesWaitLedger is an in-memory crossscope.ReadinessWaitLedger.
type usesWaitLedger struct {
	rows    map[string]crossscope.ReadinessWait
	upserts int
}

func (l *usesWaitLedger) GetReadinessWait(_ context.Context, scopeID string, domain reducercontract.Domain) (crossscope.ReadinessWait, bool, error) {
	row, ok := l.rows[scopeID+"|"+string(domain)]
	return row, ok, nil
}

func (l *usesWaitLedger) UpsertReadinessWait(_ context.Context, wait crossscope.ReadinessWait, resetAnchor bool) error {
	l.upserts++
	key := wait.ScopeID + "|" + string(wait.Domain)
	if existing, ok := l.rows[key]; ok && !resetAnchor && existing.FirstDeferredAt.Before(wait.FirstDeferredAt) {
		wait.FirstDeferredAt = existing.FirstDeferredAt
	}
	l.rows[key] = wait
	return nil
}

func (l *usesWaitLedger) ClearReadinessWait(_ context.Context, scopeID string, domain reducercontract.Domain) error {
	delete(l.rows, scopeID+"|"+string(domain))
	return nil
}

func requireInstancesNotReady(t *testing.T, err error) {
	t.Helper()
	var classified interface {
		Retryable() bool
		FailureClass() string
	}
	if err == nil || !errors.As(err, &classified) || !classified.Retryable() ||
		classified.FailureClass() != workloadinstance.NotReadyFailureClass {
		t.Fatalf("Handle() error = %v, want the retryable %s defer", err, workloadinstance.NotReadyFailureClass)
	}
}

// TestWorkloadCloudRelationshipCommitsBeforeWaitingOnInstances is §4.5 item 7
// for USES: the first evaluation commits every row (the missing stage anchor
// is a MATCH no-op) and then defers; an unchanged poll touches neither the
// graph nor the fact store; once the stage instance exists the next
// evaluation re-commits once and clears the ledger.
func TestWorkloadCloudRelationshipCommitsBeforeWaitingOnInstances(t *testing.T) {
	t.Parallel()
	clock := time.Date(2026, time.September, 19, 9, 0, 0, 0, time.UTC)
	ledger := &usesWaitLedger{rows: map[string]crossscope.ReadinessWait{}}
	lookup := &fakeWorkloadInstanceExistence{existing: map[workloadinstance.Anchor]struct{}{prodAnchor: {}}}
	first := &recordingWorkloadCloudRelationshipWriter{}
	handler := instanceReadinessHandler(lookup, first)
	handler.ReadinessWaits = ledger
	handler.Now = func() time.Time { return clock }
	factLoader := handler.FactLoader.(*stubFactLoader)
	intent := instanceReadinessIntent(clock)

	result, err := handler.Handle(context.Background(), intent)
	requireInstancesNotReady(t, err)
	if first.writeCalls != 1 || len(first.writtenRows) != 2 || first.retractCalls != 1 {
		t.Fatalf("first evaluation: writes %d rows %d retracts %d, want the commit (1/2/1) before the defer",
			first.writeCalls, len(first.writtenRows), first.retractCalls)
	}
	if result.CanonicalWrites != 0 {
		t.Fatalf("deferred Result = %+v, want the zero result that accompanies a retryable error", result)
	}

	clock = clock.Add(30 * time.Second)
	poll := &recordingWorkloadCloudRelationshipWriter{}
	handler.EdgeWriter = poll
	loads := factLoader.calls
	_, err = handler.Handle(context.Background(), intent)
	requireInstancesNotReady(t, err)
	if poll.writeCalls != 0 || poll.retractCalls != 0 || factLoader.calls != loads {
		t.Fatalf("unchanged poll: writes %d retracts %d fact loads %d, want 0/0/0",
			poll.writeCalls, poll.retractCalls, factLoader.calls-loads)
	}

	clock = clock.Add(30 * time.Second)
	lookup.existing = map[workloadinstance.Anchor]struct{}{prodAnchor: {}, stageAnchor: {}}
	resolved := &recordingWorkloadCloudRelationshipWriter{}
	handler.EdgeWriter = resolved
	result, err = handler.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("Handle() after the instance appeared error = %v", err)
	}
	if resolved.writeCalls != 1 || len(resolved.writtenRows) != 2 || result.CanonicalWrites != 2 {
		t.Fatalf("resolve re-commit: writes %d rows %d CanonicalWrites %d, want one re-commit of 2",
			resolved.writeCalls, len(resolved.writtenRows), result.CanonicalWrites)
	}
	if len(ledger.rows) != 0 {
		t.Fatalf("ledger rows = %v, want the row cleared once nothing is missing", ledger.rows)
	}
}

// TestWorkloadCloudRelationshipInstanceWaitSettlesAcrossGenerations is the
// R2-F1 closing shape for USES at the handler: the anchor survives gen-2's
// supersession, the wait settles once at anchor+MaxWait (abandoned = 1), and
// gen-3 with the same missing anchor commits at its first claim without
// another deferred count.
func TestWorkloadCloudRelationshipInstanceWaitSettlesAcrossGenerations(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	start := time.Date(2026, time.September, 19, 9, 0, 0, 0, time.UTC)
	clock := start
	lookup := &fakeWorkloadInstanceExistence{existing: map[workloadinstance.Anchor]struct{}{prodAnchor: {}}}
	handler := instanceReadinessHandler(lookup, &recordingWorkloadCloudRelationshipWriter{})
	handler.ReadinessWaits = &usesWaitLedger{rows: map[string]crossscope.ReadinessWait{}}
	handler.ReadinessMaxWait = 10 * time.Minute
	handler.Instruments = instruments
	handler.Now = func() time.Time { return clock }

	gen := func(id string, cycle time.Time) Intent {
		intent := instanceReadinessIntent(cycle)
		intent.GenerationID = id
		return intent
	}
	_, err = handler.Handle(context.Background(), gen("gen-1", start))
	requireInstancesNotReady(t, err)
	clock = start.Add(6 * time.Minute)
	gen2 := gen("gen-2", clock)
	_, err = handler.Handle(context.Background(), gen2)
	requireInstancesNotReady(t, err)
	clock = start.Add(10 * time.Minute)
	if _, err := handler.Handle(context.Background(), gen2); err != nil {
		t.Fatalf("gen-2 at anchor+MaxWait error = %v, want the wait to settle", err)
	}
	deferred := usesWaitSum(t, reader, crossscope.ReadinessWaitDeferred)
	if got := usesWaitSum(t, reader, crossscope.ReadinessWaitAbandoned); got != 1 {
		t.Fatalf("abandoned = %d, want 1", got)
	}
	clock = start.Add(40 * time.Minute)
	gen3 := &recordingWorkloadCloudRelationshipWriter{}
	handler.EdgeWriter = gen3
	if _, err := handler.Handle(context.Background(), gen("gen-3", clock)); err != nil {
		t.Fatalf("gen-3 first claim error = %v, want success on the settled set", err)
	}
	if gen3.writeCalls != 1 || usesWaitSum(t, reader, crossscope.ReadinessWaitDeferred) != deferred ||
		usesWaitSum(t, reader, crossscope.ReadinessWaitSettledMissing) != 1 {
		t.Fatalf("gen-3: writes %d, want a commit counted as settled_missing with deferred unchanged", gen3.writeCalls)
	}
}

func usesWaitSum(t *testing.T, reader *sdkmetric.ManualReader, outcome string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	var total int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			if m.Name != "eshu_dp_reducer_readiness_waits_total" || !ok {
				continue
			}
			for _, point := range sum.DataPoints {
				domain, _ := point.Attributes.Value("domain")
				value, _ := point.Attributes.Value("outcome")
				if domain.AsString() == string(DomainWorkloadCloudRelationshipMaterialization) && value.AsString() == outcome {
					total += point.Value
				}
			}
		}
	}
	return total
}
