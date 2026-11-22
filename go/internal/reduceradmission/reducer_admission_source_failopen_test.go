// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reduceradmission

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// failingSourceChecker fails the consult for the named sources and reports
// every other source clear. It counts consults per source.
type failingSourceChecker struct {
	mu     sync.Mutex
	failed map[string]error
	calls  map[string]int
}

func (c *failingSourceChecker) CheckSourceBackpressureForSource(
	_ context.Context,
	source string,
	_ workflow.SourceBackpressureConfig,
	_ time.Time,
) (workflow.SourceBackpressureDecision, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.calls == nil {
		c.calls = map[string]int{}
	}
	c.calls[source]++
	if err := c.failed[source]; err != nil {
		return workflow.SourceBackpressureDecision{SourceSystem: source}, err
	}
	return workflow.SourceBackpressureDecision{SourceSystem: source}, nil
}

func newFailOpenInstruments(t *testing.T) (*telemetry.Instruments, sdkmetric.Reader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return inst, reader
}

// sourceCounter sums one counter's data points whose source_system label
// equals source ("" sums every point).
func sourceCounter(t *testing.T, reader sdkmetric.Reader, name, source string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			if m.Name != name || !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				if source == "" {
					total += dp.Value
					continue
				}
				if v, ok := dp.Attributes.Value("source_system"); ok && v.AsString() == source {
					total += dp.Value
				}
			}
		}
	}
	return total
}

func failOpenWriter(inner runtime.ReducerIntentWriter, checker reducerAdmissionSourceChecker, inst *telemetry.Instruments) reducerAdmissionWriter {
	return reducerAdmissionWriter{
		inner: inner,
		depthReader: &fakeReducerAdmissionDepthReader{
			depths: []map[string]map[string]int64{{"reducer": {"pending": 1}}},
		},
		config:        reducerAdmissionConfig{HighWaterMark: 10_000, PollInterval: time.Second},
		deferral:      newAdmissionDeferralState(),
		sourceChecker: checker,
		sourceConfig:  admissionSourceTestConfig(),
		instruments:   inst,
		sleep:         func(context.Context, time.Duration) error { return nil },
	}
}

// TestReducerAdmissionSourceConsultErrorFailsOpen proves #10882 F1: an
// unreadable per-source signal is not evidence of a backlog, so the group
// admits (never a terminal, non-retryable error that dead-letters after the
// canonical and content writes ran), the failure is counted per source, and
// the mixed batch still lands every group.
func TestReducerAdmissionSourceConsultErrorFailsOpen(t *testing.T) {
	t.Parallel()

	inst, reader := newFailOpenInstruments(t)
	writer := &recordingReducerIntentWriter{}
	checker := &failingSourceChecker{failed: map[string]error{
		"git": errors.New("canceling statement due to statement timeout"),
	}}
	admission := failOpenWriter(writer, checker, inst)
	intents := []runtime.ReducerIntent{
		{Domain: reducer.DomainWorkloadIdentity, SourceSystem: "git", EntityKey: "git-1"},
		{Domain: reducer.DomainWorkloadIdentity, SourceSystem: "aws", EntityKey: "aws-1"},
	}

	result, err := admission.Enqueue(context.Background(), intents)
	if err != nil {
		t.Fatalf("Enqueue() error = %v, want nil: an unreadable signal must fail open", err)
	}
	if result.Count != len(intents) || writer.calls != 2 {
		t.Fatalf("Enqueue() = (count %d, inner calls %d), want (%d, 2): every group reaches the inner writer",
			result.Count, writer.calls, len(intents))
	}
	if got := sourceCounter(t, reader, "eshu_dp_reducer_admission_source_consult_failures_total", "git"); got != 1 {
		t.Fatalf("consult failures{source_system=git} = %d, want 1", got)
	}
	if got := sourceCounter(t, reader, "eshu_dp_reducer_admission_source_consult_failures_total", "aws"); got != 0 {
		t.Fatalf("consult failures{source_system=aws} = %d, want 0 (healthy consult)", got)
	}
	if got := sourceCounter(t, reader, "eshu_dp_reducer_admission_deferrals_total", ""); got != 0 {
		t.Fatalf("deferrals = %d, want 0: a failed consult is not a deferral", got)
	}
}

// TestReducerAdmissionSourceConsultCancelledContextReturnsContextError pins
// that a cancelled context is shutdown, not a signal failure: it returns the
// context error and counts nothing.
func TestReducerAdmissionSourceConsultCancelledContextReturnsContextError(t *testing.T) {
	t.Parallel()

	inst, reader := newFailOpenInstruments(t)
	writer := &recordingReducerIntentWriter{}
	checker := &failingSourceChecker{failed: map[string]error{"git": context.Canceled}}
	admission := failOpenWriter(writer, checker, inst)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := admission.Enqueue(ctx, []runtime.ReducerIntent{
		{Domain: reducer.DomainWorkloadIdentity, SourceSystem: "git", EntityKey: "git-1"},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Enqueue() error = %v, want context.Canceled", err)
	}
	if writer.calls != 0 {
		t.Fatalf("inner calls = %d, want 0 after cancellation", writer.calls)
	}
	if got := sourceCounter(t, reader, "eshu_dp_reducer_admission_source_consult_failures_total", ""); got != 0 {
		t.Fatalf("consult failures = %d, want 0 on a cancelled context", got)
	}
}
