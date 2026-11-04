// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"bytes"
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// probeStubLoader answers the candidate probe with a fixed verdict or error
// while counting bulk loads, so tests can prove the fail-open fallback.
type probeStubLoader struct {
	*stubFactLoader
	probeErr error
}

func (p probeStubLoader) ScopeMayHoldWorkloadCandidates(_ context.Context, _, _ string) (bool, error) {
	return p.mayHoldCandidates, p.probeErr
}

// sqlStateError mimics the SQLState() accessor pgconn.PgError exposes.
type sqlStateError struct{ code string }

func (e sqlStateError) Error() string    { return "ERROR: probe failed (SQLSTATE " + e.code + ")" }
func (e sqlStateError) SQLState() string { return e.code }

// lockedBuffer lets the JSON log handler write while the test reads.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func probeCounterValue(t *testing.T, reader *sdkmetric.ManualReader, outcome, cause string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var total int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_reducer_workload_candidate_probe_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("probe metric data = %T, want Sum[int64]", m.Data)
			}
			for _, dp := range sum.DataPoints {
				got := map[string]string{}
				for _, attr := range dp.Attributes.ToSlice() {
					got[string(attr.Key)] = attr.Value.AsString()
				}
				if len(got) != 2 {
					t.Fatalf("probe metric attributes = %v, want only outcome and cause_class", got)
				}
				if got["outcome"] == outcome && got["cause_class"] == cause {
					total += dp.Value
				}
			}
		}
	}
	return total
}

// TestPregateProbeRecordsOutcomeCounter pins the #9986 operator signal: every
// probe call increments eshu_dp_reducer_workload_candidate_probe_total with a
// closed outcome and cause_class, so a probe that always errors is a visible
// error rate instead of a silent fallback.
func TestPregateProbeRecordsOutcomeCounter(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		mayHold bool
		err     error
		outcome string
		cause   string
	}{
		{"may hold", true, nil, telemetry.WorkloadCandidateProbeOutcomeMayHold, telemetry.WorkloadCandidateProbeCauseNone},
		{"no candidates", false, nil, telemetry.WorkloadCandidateProbeOutcomeNoCandidates, telemetry.WorkloadCandidateProbeCauseNone},
		{"sql invalid", false, sqlStateError{code: "42601"}, telemetry.WorkloadCandidateProbeOutcomeError, telemetry.WorkloadCandidateProbeCauseSQLInvalid},
		{"canceled", false, fmt.Errorf("probe: %w", context.Canceled), telemetry.WorkloadCandidateProbeOutcomeError, telemetry.WorkloadCandidateProbeCauseCanceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			reader := sdkmetric.NewManualReader()
			instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
			if err != nil {
				t.Fatalf("NewInstruments() error = %v", err)
			}
			loader := CorrelatedWorkloadProjectionInputLoader{
				FactLoader:    probeStubLoader{stubFactLoader: &stubFactLoader{mayHoldCandidates: tc.mayHold}, probeErr: tc.err},
				Instruments:   instruments,
				Logger:        slog.New(slog.NewTextHandler(&lockedBuffer{}, nil)),
				probeErrorLog: newProbeErrorLogLimiter(time.Minute, time.Now),
			}

			if err := loader.deferUnlessReadyForBulkLoad(context.Background(), dockerfileLoaderIntent()); err != nil {
				t.Fatalf("deferUnlessReadyForBulkLoad() error = %v, want nil with no lookups wired", err)
			}
			if got := probeCounterValue(t, reader, tc.outcome, tc.cause); got != 1 {
				t.Fatalf("probe counter {outcome=%s cause_class=%s} = %d, want 1", tc.outcome, tc.cause, got)
			}
		})
	}
}

// TestPregateProbeErrorFallsBackToBulkLoad keeps the fail-open contract: a
// probe error must still let the normal load-then-gate path run, so truth is
// unchanged while the failure becomes observable.
func TestPregateProbeErrorFallsBackToBulkLoad(t *testing.T) {
	t.Parallel()

	stub := &stubFactLoader{envelopes: dockerfileLoaderEnvelopes()}
	loader := CorrelatedWorkloadProjectionInputLoader{
		FactLoader:     probeStubLoader{stubFactLoader: stub, probeErr: sqlStateError{code: "42601"}},
		ResolvedLoader: &stubResolvedRelationshipLoader{},
		Logger:         slog.New(slog.NewTextHandler(&lockedBuffer{}, nil)),
		probeErrorLog:  newProbeErrorLogLimiter(time.Minute, time.Now),
	}
	if _, err := loader.LoadWorkloadProjectionScopeInputs(context.Background(), dockerfileLoaderIntent()); err != nil {
		t.Fatalf("LoadWorkloadProjectionScopeInputs() error = %v, want the normal load to succeed", err)
	}
	if stub.calls != 1 {
		t.Fatalf("fact loader calls = %d, want 1: a probe error must proceed to the bulk load", stub.calls)
	}
}

// TestPregateProbeErrorLogsOncePerCauseClass proves the error log is rate
// limited per cause class: a probe that fails on every intent must not flood
// the reducer log, while a new cause class, and the same class after the
// interval, still surface with the suppressed count.
func TestPregateProbeErrorLogsOncePerCauseClass(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	out := &lockedBuffer{}
	loader := CorrelatedWorkloadProjectionInputLoader{
		FactLoader:    probeStubLoader{stubFactLoader: &stubFactLoader{}, probeErr: sqlStateError{code: "42601"}},
		Logger:        slog.New(slog.NewJSONHandler(out, nil)),
		probeErrorLog: newProbeErrorLogLimiter(10*time.Minute, clock),
	}
	ctx := context.Background()
	intent := dockerfileLoaderIntent()

	for range 50 {
		_ = loader.deferUnlessReadyForBulkLoad(ctx, intent)
	}
	if got := strings.Count(out.String(), "workload candidate probe failed"); got != 1 {
		t.Fatalf("log lines after 50 same-class errors = %d, want 1\n%s", got, out.String())
	}
	line := out.String()
	for _, want := range []string{`"failure_class":"sql_invalid"`, `"sqlstate":"42601"`, `"scope_id":"scope-service"`, `"generation_id":"gen-1"`} {
		if !strings.Contains(line, want) {
			t.Errorf("first error log misses %s:\n%s", want, line)
		}
	}

	// A different cause class logs immediately.
	loader.FactLoader = probeStubLoader{stubFactLoader: &stubFactLoader{}, probeErr: context.DeadlineExceeded}
	_ = loader.deferUnlessReadyForBulkLoad(ctx, intent)
	if got := strings.Count(out.String(), "workload candidate probe failed"); got != 2 {
		t.Fatalf("log lines after a new cause class = %d, want 2", got)
	}

	// The same class logs again after the interval and reports what it hid.
	now = now.Add(11 * time.Minute)
	loader.FactLoader = probeStubLoader{stubFactLoader: &stubFactLoader{}, probeErr: sqlStateError{code: "42601"}}
	_ = loader.deferUnlessReadyForBulkLoad(ctx, intent)
	if got := strings.Count(out.String(), "workload candidate probe failed"); got != 3 {
		t.Fatalf("log lines after the interval = %d, want 3", got)
	}
	if !strings.Contains(out.String(), `"suppressed":49`) {
		t.Fatalf("re-logged error misses suppressed=49:\n%s", out.String())
	}
}

// timeoutError is a net.Error timeout that is not context.DeadlineExceeded,
// the shape pgx's own connect timeout surfaces.
type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

// TestClassifyWorkloadCandidateProbeError pins the closed cause-class mapping.
func TestClassifyWorkloadCandidateProbeError(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		err      error
		cause    string
		sqlstate string
	}{
		{"syntax error", sqlStateError{code: "42601"}, telemetry.WorkloadCandidateProbeCauseSQLInvalid, "42601"},
		{"wrapped syntax error", fmt.Errorf("probe: %w", sqlStateError{code: "42883"}), telemetry.WorkloadCandidateProbeCauseSQLInvalid, "42883"},
		{"connection failure", sqlStateError{code: "08006"}, telemetry.WorkloadCandidateProbeCauseConnection, "08006"},
		{"out of memory", sqlStateError{code: "53200"}, telemetry.WorkloadCandidateProbeCauseResource, "53200"},
		{"statement canceled by server", sqlStateError{code: "57014"}, telemetry.WorkloadCandidateProbeCauseResource, "57014"},
		{"unclassified sqlstate", sqlStateError{code: "XX000"}, telemetry.WorkloadCandidateProbeCauseOther, "XX000"},
		{"context canceled", context.Canceled, telemetry.WorkloadCandidateProbeCauseCanceled, ""},
		{"deadline", fmt.Errorf("probe: %w", context.DeadlineExceeded), telemetry.WorkloadCandidateProbeCauseDeadline, ""},
		{"plain error", errors.New("boom"), telemetry.WorkloadCandidateProbeCauseOther, ""},
		{"connection reset mid-query", fmt.Errorf("probe: %w", &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}), telemetry.WorkloadCandidateProbeCauseConnection, ""},
		{"driver bad connection", fmt.Errorf("probe: %w", driver.ErrBadConn), telemetry.WorkloadCandidateProbeCauseConnection, ""},
		{"connection closed", fmt.Errorf("probe: %w", pgconn.ErrConnClosed), telemetry.WorkloadCandidateProbeCauseConnection, ""},
		{"network timeout that is not a context deadline", &net.OpError{Op: "dial", Net: "tcp", Err: timeoutError{}}, telemetry.WorkloadCandidateProbeCauseConnection, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cause, sqlstate := classifyWorkloadCandidateProbeError(tc.err)
			if cause != tc.cause || sqlstate != tc.sqlstate {
				t.Fatalf("classify(%v) = (%q, %q), want (%q, %q)", tc.err, cause, sqlstate, tc.cause, tc.sqlstate)
			}
		})
	}
}
