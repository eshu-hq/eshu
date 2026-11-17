// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	settlementEvictionMetric  = "eshu_dp_nvd_baseline_settlement_evictions_total"
	settlementEvictionMessage = "nvd baseline settlement evicted at capacity, post-commit signals for that chunk may be lost"
	settlementCommittedLog    = "nvd baseline chunk committed"
)

// settlementClock returns a clock that advances one second per call, so the
// oldest stashed settlement is the first one stashed and the eviction order is
// deterministic. It is safe for concurrent stashes.
func settlementClock() func() time.Time {
	var mu sync.Mutex
	now := time.Date(2026, 11, 17, 8, 0, 0, 0, time.UTC)
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		now = now.Add(time.Second)
		return now
	}
}

// settlementEvictions sums the eviction counter and fails if it carries any
// label: the counter is unlabeled by design, so no work item or run id can
// become a label value.
func settlementEvictions(t *testing.T, reader *sdkmetric.ManualReader) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var total int64
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok || m.Name != settlementEvictionMetric {
				continue
			}
			for _, point := range sum.DataPoints {
				if point.Attributes.Len() != 0 {
					t.Fatalf("%s carries attributes %v, want none", settlementEvictionMetric, point.Attributes)
				}
				total += point.Value
			}
		}
	}
	return total
}

// settlementLogRecords returns the JSON log records with the given message.
func settlementLogRecords(t *testing.T, logs *bytes.Buffer, message string) []map[string]any {
	t.Helper()
	var matched []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("log line %q is not JSON: %v", line, err)
		}
		if record["msg"] == message {
			matched = append(matched, record)
		}
	}
	return matched
}

// armSettlement makes a fresh work item and arms the checkpoint writer for it
// without running the writer. Arming is a new policy decision, which discards
// any settlement an earlier decision for the same item left in the stash.
func armSettlement(
	t *testing.T, policy *NVDBaselineCommitPolicy, id string,
) (workflow.WorkItem, func(context.Context, *affectedCommitExecutor) error) {
	t.Helper()
	item := testWorkItem()
	item.WorkItemID = id
	item.ScopeID = testNVDBaselineScopeForCommit
	return item, armedBaselineWriter(t, policy, item)
}

// stashSettlement arms the checkpoint writer for a fresh work item and runs it,
// which stashes the item's settlement the way the commit transaction does.
func stashSettlement(t *testing.T, policy *NVDBaselineCommitPolicy, id string) workflow.WorkItem {
	t.Helper()
	item, writer := armSettlement(t, policy, id)
	if err := writer(context.Background(), &affectedCommitExecutor{}); err != nil {
		t.Fatalf("writer for %s error = %v", id, err)
	}
	return item
}

// TestNVDBaselineSettlementEvictionCountsAndNamesTheDroppedItem pins the signal
// the review of #10835 asked for (eshu-hq/eshu#10746): when the full stash
// evicts the oldest settlement to take a new one, the eviction is counted and a
// WARN names the dropped work item, never the incoming one and never a scope.
// The dropped chunk's forward-rotation count and committed-chunk line are the
// only things lost, and the counter is how an operator reconciles them.
func TestNVDBaselineSettlementEvictionCountsAndNamesTheDroppedItem(t *testing.T) {
	t.Parallel()

	policy, reader, logs := baselineSeedPolicy(t)
	policy.clock = settlementClock()
	items := make([]workflow.WorkItem, 0, maxBaselineSettlements)
	writers := make([]func(context.Context, *affectedCommitExecutor) error, 0, maxBaselineSettlements)
	for i := 0; i < maxBaselineSettlements; i++ {
		item, writer := armSettlement(t, policy, fmt.Sprintf("work-item-%03d", i))
		if err := writer(context.Background(), &affectedCommitExecutor{}); err != nil {
			t.Fatalf("writer for %s error = %v", item.WorkItemID, err)
		}
		items = append(items, item)
		writers = append(writers, writer)
	}
	if got := settlementEvictions(t, reader); got != 0 || len(settlementLogRecords(t, logs, settlementEvictionMessage)) != 0 {
		t.Fatalf("a full stash reported %d evictions, want none until a new key arrives", got)
	}

	overflow := stashSettlement(t, policy, "work-item-overflow")
	if got := settlementEvictions(t, reader); got != 1 {
		t.Fatalf("evictions = %d, want 1", got)
	}
	records := settlementLogRecords(t, logs, settlementEvictionMessage)
	if len(records) != 1 {
		t.Fatalf("eviction WARN count = %d, want 1; logs: %s", len(records), logs.String())
	}
	record := records[0]
	if record["level"] != "WARN" {
		t.Fatalf("eviction log level = %v, want WARN", record["level"])
	}
	if record["work_item_id"] != items[0].WorkItemID {
		t.Fatalf("WARN work_item_id = %v, want the evicted oldest item %q, not the incoming %q",
			record["work_item_id"], items[0].WorkItemID, overflow.WorkItemID)
	}
	for _, key := range []string{"baseline_run_id", "year", "collector_instance_id", "capacity"} {
		if _, ok := record[key]; !ok {
			t.Fatalf("eviction WARN lacks %q: %v", key, record)
		}
	}
	line, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}
	if strings.Contains(string(line), testNVDBaselineScopeForCommit) {
		t.Fatalf("eviction WARN carries the scope id: %s", line)
	}

	// The same writer running again for a key the stash already holds (a commit
	// transaction retried under one decision) replaces the row in place and is
	// never an eviction. Arming a new decision would discard the held row first,
	// so this reuses the closure the first run used.
	if err := writers[5](context.Background(), &affectedCommitExecutor{}); err != nil {
		t.Fatalf("re-running the writer for %s error = %v", items[5].WorkItemID, err)
	}
	if got := settlementEvictions(t, reader); got != 1 {
		t.Fatalf("re-stashing a held key counted an eviction: %d, want 1", got)
	}

	// The evicted chunk lost its post-commit signal and every other chunk kept
	// its own: 65 distinct chunks, 64 forward rotations counted, one eviction.
	policy.SettleClaimCommitted(context.Background(), items[0])
	if got := seedRotationCounts(t, reader)["forward"]; got != 0 {
		t.Fatalf("the evicted settlement still counted a forward rotation: %d", got)
	}
	for _, item := range items[1:] {
		policy.SettleClaimCommitted(context.Background(), item)
	}
	policy.SettleClaimCommitted(context.Background(), overflow)
	if got := seedRotationCounts(t, reader)["forward"]; got != maxBaselineSettlements {
		t.Fatalf("forward rotations = %d, want %d: 65 chunks minus the one eviction", got, maxBaselineSettlements)
	}
	committed := settlementLogRecords(t, logs, settlementCommittedLog)
	if len(committed) != maxBaselineSettlements {
		t.Fatalf("committed-chunk lines = %d, want %d", len(committed), maxBaselineSettlements)
	}
	for _, row := range committed {
		if row["work_item_id"] == items[0].WorkItemID {
			t.Fatalf("the evicted item still logged a committed chunk: %v", row)
		}
	}
}

// TestNVDBaselineSettlementEvictionBreaksTiesByWorkItemID pins that the eviction
// never depends on map iteration order: settlements stashed at one instant fall
// to the smaller work item id. A coarse clock makes equal times plausible, and a
// random pick would drop a different chunk on every run.
func TestNVDBaselineSettlementEvictionBreaksTiesByWorkItemID(t *testing.T) {
	t.Parallel()

	frozen := time.Date(2026, 11, 17, 8, 0, 0, 0, time.UTC)
	for attempt := 0; attempt < 20; attempt++ {
		policy, _, logs := baselineSeedPolicy(t)
		policy.clock = func() time.Time { return frozen }
		// Descending ids, so the first inserted is not the smallest.
		for i := maxBaselineSettlements - 1; i >= 0; i-- {
			stashSettlement(t, policy, fmt.Sprintf("work-item-%03d", i))
		}
		stashSettlement(t, policy, "work-item-overflow")
		records := settlementLogRecords(t, logs, settlementEvictionMessage)
		if len(records) != 1 || records[0]["work_item_id"] != "work-item-000" {
			t.Fatalf("attempt %d evicted %v, want the smallest id work-item-000", attempt, records)
		}
	}
}

// reentrantWarnHandler re-enters a callback on every WARN, as a slow or
// synchronous log sink might, then writes through.
type reentrantWarnHandler struct {
	slog.Handler
	onWarn func()
}

func (h reentrantWarnHandler) Handle(ctx context.Context, record slog.Record) error {
	if record.Level == slog.LevelWarn {
		h.onWarn()
	}
	return h.Handler.Handle(ctx, record)
}

func (h reentrantWarnHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return reentrantWarnHandler{Handler: h.Handler.WithAttrs(attrs), onWarn: h.onWarn}
}

func (h reentrantWarnHandler) WithGroup(name string) slog.Handler {
	return reentrantWarnHandler{Handler: h.Handler.WithGroup(name), onWarn: h.onWarn}
}

// TestNVDBaselineSettlementEvictionReportsOutsideTheStashLock pins that the
// counter and WARN run after the stash lock is released. Every commit worker
// shares that lock, so a log sink that blocks, or one that re-enters the stash
// as this handler does, would otherwise stall or deadlock the commit path.
func TestNVDBaselineSettlementEvictionReportsOutsideTheStashLock(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	var logs bytes.Buffer
	var policy *NVDBaselineCommitPolicy
	handler := reentrantWarnHandler{
		Handler: slog.NewJSONHandler(&logs, nil),
		onWarn:  func() { policy.DiscardClaimSettlement(workflow.WorkItem{WorkItemID: "reentrant"}) },
	}
	policy, err = NewNVDBaselineCommitPolicy("scorecard-instance", instruments, slog.New(handler))
	if err != nil {
		t.Fatalf("NewNVDBaselineCommitPolicy() error = %v", err)
	}
	policy.clock = settlementClock()
	for i := 0; i < maxBaselineSettlements; i++ {
		stashSettlement(t, policy, fmt.Sprintf("work-item-%03d", i))
	}
	overflow := testWorkItem()
	overflow.WorkItemID = "work-item-overflow"
	overflow.ScopeID = testNVDBaselineScopeForCommit
	writer := armedBaselineWriter(t, policy, overflow)

	done := make(chan error, 1)
	go func() { done <- writer(context.Background(), &affectedCommitExecutor{}) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("writer error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the eviction report deadlocked: it ran while the stash lock was held")
	}
	if got := settlementEvictions(t, reader); got != 1 {
		t.Fatalf("evictions = %d, want 1", got)
	}
}

// TestNVDBaselineSettlementEvictionCountsExactlyUnderConcurrentStashes stashes
// distinct work items from many goroutines at once. Every stash of a new key at
// capacity evicts exactly one row, so the evictions are the stashes minus the
// capacity whatever the interleaving, the stash never grows past its bound, and
// every eviction is reported once. It is meaningful under the race detector.
func TestNVDBaselineSettlementEvictionCountsExactlyUnderConcurrentStashes(t *testing.T) {
	t.Parallel()

	policy, reader, logs := baselineSeedPolicy(t)
	policy.clock = settlementClock()
	const total = 200
	writers := make([]func(context.Context, *affectedCommitExecutor) error, total)
	for i := range writers {
		item := testWorkItem()
		item.WorkItemID = fmt.Sprintf("work-item-%03d", i)
		item.ScopeID = testNVDBaselineScopeForCommit
		writers[i] = armedBaselineWriter(t, policy, item)
	}
	var wg sync.WaitGroup
	errs := make(chan error, total)
	for _, writer := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- writer(context.Background(), &affectedCommitExecutor{})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("writer error = %v", err)
		}
	}

	want := int64(total - maxBaselineSettlements)
	if got := settlementEvictions(t, reader); got != want {
		t.Fatalf("evictions = %d, want %d", got, want)
	}
	records := settlementLogRecords(t, logs, settlementEvictionMessage)
	seen := map[any]bool{}
	for _, record := range records {
		seen[record["work_item_id"]] = true
	}
	if int64(len(records)) != want || int64(len(seen)) != want {
		t.Fatalf("eviction WARNs = %d over %d distinct items, want %d each", len(records), len(seen), want)
	}
	policy.settlements.mu.Lock()
	held := len(policy.settlements.rows)
	policy.settlements.mu.Unlock()
	if held != maxBaselineSettlements {
		t.Fatalf("stash holds %d settlements, want exactly the bound %d", held, maxBaselineSettlements)
	}
}

// TestNVDBaselineSettlementEvictionWithoutSignalsStillBounds pins that a policy
// built with no instruments and no logger (the optional wiring) evicts without
// panicking and keeps the stash bounded.
func TestNVDBaselineSettlementEvictionWithoutSignalsStillBounds(t *testing.T) {
	t.Parallel()

	policy, err := NewNVDBaselineCommitPolicy("scorecard-instance", nil, nil)
	if err != nil {
		t.Fatalf("NewNVDBaselineCommitPolicy() error = %v", err)
	}
	policy.clock = settlementClock()
	for i := 0; i < maxBaselineSettlements+10; i++ {
		stashSettlement(t, policy, fmt.Sprintf("work-item-%03d", i))
	}
	policy.settlements.mu.Lock()
	held := len(policy.settlements.rows)
	policy.settlements.mu.Unlock()
	if held != maxBaselineSettlements {
		t.Fatalf("stash holds %d settlements, want the bound %d", held, maxBaselineSettlements)
	}
}

// BenchmarkNVDBaselineSettlementEvictionReport measures the work an eviction
// adds after the stash lock is released: one counter add and one WARN record.
// An eviction needs a full stash, which only happens when more than 64 chunks
// wait for their commit hook at once, so this runs rarely and off the lock.
func BenchmarkNVDBaselineSettlementEvictionReport(b *testing.B) {
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewManualReader()))
	instruments, err := telemetry.NewInstruments(provider.Meter("bench"))
	if err != nil {
		b.Fatalf("NewInstruments() error = %v", err)
	}
	policy, err := NewNVDBaselineCommitPolicy("scorecard-instance", instruments, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err != nil {
		b.Fatalf("NewNVDBaselineCommitPolicy() error = %v", err)
	}
	evicted := baselineSettlement{runID: "baseline-2026-11", year: 2002}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		policy.reportSettlementEviction(ctx, "work-item-000", evicted)
	}
}
