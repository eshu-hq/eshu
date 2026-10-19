// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossrepo"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// recordedExecution captures the one log line and the status counter a
// recordReducerResult call emits.
type recordedExecution struct {
	level, msg, failureClass, status string
	deferred, failed                 int64
}

func recordOneFailure(t *testing.T, isDeferred func(string) bool, execErr error) recordedExecution {
	t.Helper()
	var logs bytes.Buffer
	reader := metric.NewManualReader()
	instruments, err := telemetry.NewInstruments(metric.NewMeterProvider(metric.WithReader(reader)).Meter("reducer-observability"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	service := Service{
		Logger:                 slog.New(slog.NewJSONHandler(&logs, nil)),
		Instruments:            instruments,
		IsDeferredFailureClass: isDeferred,
	}
	intent := reducercontract.Intent{IntentID: "intent-1", Domain: DomainDeployableUnitCorrelation, EntityKeys: []string{"repo:a"}}
	service.recordReducerResult(context.Background(), intent, Result{}, 0.1, 0.2, "failed", 1, execErr)

	var line map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &line); err != nil {
		t.Fatalf("decode log line %q: %v", logs.String(), err)
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	attrs := func(status string) map[string]string {
		return map[string]string{"queue": "reducer", "status": status, "domain": string(intent.Domain)}
	}
	got := recordedExecution{
		deferred: executionCount(rm, attrs("deferred")),
		failed:   executionCount(rm, attrs("failed")),
	}
	got.level, _ = line["level"].(string)
	got.msg, _ = line["msg"].(string)
	got.failureClass, _ = line["failure_class"].(string)
	got.status, _ = line["status"].(string)
	return got
}

// executionCount sums eshu_dp_reducer_executions_total points whose attributes
// match want exactly; zero when none match.
func executionCount(rm metricdata.ResourceMetrics, want map[string]string) int64 {
	var total int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			if m.Name != "eshu_dp_reducer_executions_total" || !ok {
				continue
			}
			for _, point := range sum.DataPoints {
				if point.Attributes.Len() != len(want) {
					continue
				}
				match := true
				for key, value := range want {
					if got, ok := point.Attributes.Value(attribute.Key(key)); !ok || got.AsString() != value {
						match = false
					}
				}
				if match {
					total += point.Value
				}
			}
		}
	}
	return total
}

func onlyResolutionDeferral(class string) bool {
	return class == DeployableUnitCorrelationResolutionNotReadyFailureClass
}

func TestRecordReducerResultLogsNonCountingFailureAsDeferred(t *testing.T) {
	t.Parallel()
	got := recordOneFailure(t, onlyResolutionDeferral, deployableUnitCorrelationResolutionNotReadyError{
		scopeID: "scope-1", generationID: "gen-1",
	})
	if got.level != "WARN" || got.msg != "reducer execution deferred" {
		t.Fatalf("log = %s %q, want WARN \"reducer execution deferred\"", got.level, got.msg)
	}
	if got.failureClass != DeployableUnitCorrelationResolutionNotReadyFailureClass || got.status != "deferred" {
		t.Fatalf("log failure_class=%q status=%q, want the deferral class and status deferred", got.failureClass, got.status)
	}
	if got.deferred != 1 || got.failed != 0 {
		t.Fatalf("executions deferred=%d failed=%d, want 1/0", got.deferred, got.failed)
	}
}

func TestRecordReducerResultKeepsProducerTerminalFailureAsFailed(t *testing.T) {
	t.Parallel()
	gate := crossrepo.EvaluateOwnGate(context.Background(),
		func(string) (bool, error) { return false, nil },
		stubProducerLookup(crossrepo.ProducerState{
			ScopeActiveGenerationID: "gen-1", WorkItemID: "dm-1", Status: "dead_letter", FailureClass: "retry_exhausted",
		}, nil),
		reducercontract.Intent{ScopeID: "scope-1", GenerationID: "gen-1", Domain: DomainDeployableUnitCorrelation}, true, time.Now().UTC())
	if gate.Err == nil {
		t.Fatal("EvaluateOwnGate returned no producer-terminal error")
	}
	for name, predicate := range map[string]func(string) bool{"wired": onlyResolutionDeferral, "unwired": nil} {
		got := recordOneFailure(t, predicate, gate.Err)
		if got.level != "ERROR" || got.msg != "reducer execution failed" {
			t.Fatalf("%s: log = %s %q, want ERROR \"reducer execution failed\"", name, got.level, got.msg)
		}
		if got.failureClass != crossrepo.DeployableUnitCorrelationProducerDeadLetteredFailureClass || got.status != "failed" {
			t.Fatalf("%s: log failure_class=%q status=%q, want the producer-terminal class and failed",
				name, got.failureClass, got.status)
		}
		if got.deferred != 0 || got.failed != 1 {
			t.Fatalf("%s: executions deferred=%d failed=%d, want 0/1", name, got.deferred, got.failed)
		}
	}
}

func TestRecordReducerResultUnwiredPredicateKeepsDeferralFailed(t *testing.T) {
	t.Parallel()
	got := recordOneFailure(t, nil, deployableUnitCorrelationResolutionNotReadyError{scopeID: "s", generationID: "g"})
	if got.msg != "reducer execution failed" || got.status != "failed" || !strings.EqualFold(got.level, "error") {
		t.Fatalf("unwired predicate: %+v, want the pre-#7597 failed/ERROR shape", got)
	}
}
