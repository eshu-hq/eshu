// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
	"github.com/eshu-hq/eshu/go/internal/workflow"
	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
)

// recordingResultRecorder records both status rows and the per-claim result.
type recordingResultRecorder struct {
	recordingStatusRecorder
	results []ResultRecord
}

func (r *recordingResultRecorder) RecordExtensionResult(_ context.Context, record ResultRecord) {
	r.results = append(r.results, record)
}

// TestNextClaimedReportsOneResultForEveryFinalState proves every NextClaimed
// exit path reports exactly one ResultRecord with the right origin, state, and
// failure class, and that only validated SDK results carry status rows.
func TestNextClaimedReportsOneResultForEveryFinalState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		item        func() workflow.WorkItem
		result      func(workflow.WorkItem) sdkcollector.Result
		runnerErr   error
		recordErr   error
		want        ResultRecord
		wantRecords int
	}{
		{
			name:   "complete",
			result: func(item workflow.WorkItem) sdkcollector.Result { return completeResult(item, testSDKFact(item)) },
			want: ResultRecord{
				CollectorKind: "repo", Origin: ResultOriginSDK, State: sdkcollector.ResultComplete,
				StatusCount: 1, FactCount: 1,
			},
			wantRecords: 1,
		},
		{
			name:   "unchanged",
			result: unchangedBenchResult,
			want: ResultRecord{
				CollectorKind: "repo", Origin: ResultOriginSDK, State: sdkcollector.ResultUnchanged,
				StatusCount: 1, SourceLatencyMS: 250,
			},
			wantRecords: 1,
		},
		{
			name: "partial",
			result: func(item workflow.WorkItem) sdkcollector.Result {
				result := baseResult(item, sdkcollector.ResultPartial)
				result.Facts = []sdkcollector.Fact{testSDKFact(item)}
				result.Statuses = []sdkcollector.Status{
					{Class: sdkcollector.StatusWarning, Partial: true, WarningCount: 2, SourceLatencyMS: 40},
					{Class: sdkcollector.StatusComplete, FactCount: 1, SourceLatencyMS: 90},
				}
				return result
			},
			want: ResultRecord{
				CollectorKind: "repo", Origin: ResultOriginSDK, State: sdkcollector.ResultPartial,
				StatusCount: 2, FactCount: 1, WarningCount: 2, SourceLatencyMS: 90, Partial: true,
			},
			wantRecords: 2,
		},
		{
			name:   "retryable",
			result: func(item workflow.WorkItem) sdkcollector.Result { return retryableResultWithDelays(item, 30, 120) },
			want: ResultRecord{
				CollectorKind: "repo", Origin: ResultOriginSDK, State: sdkcollector.ResultRetryable,
				FailureClass: "rate_limited", StatusCount: 2, RetryAfter: 120 * time.Second, RetryAfterRequestedSeconds: 120,
			},
			wantRecords: 2,
		},
		{
			name: "terminal",
			result: func(item workflow.WorkItem) sdkcollector.Result {
				result := baseResult(item, sdkcollector.ResultTerminal)
				result.Statuses = []sdkcollector.Status{{Class: sdkcollector.StatusFailure, FailureClass: "invalid_config"}}
				return result
			},
			want: ResultRecord{
				CollectorKind: "repo", Origin: ResultOriginSDK, State: sdkcollector.ResultTerminal,
				FailureClass: "invalid_config", StatusCount: 1,
			},
			wantRecords: 1,
		},
		{
			name: "invalid claim",
			item: func() workflow.WorkItem {
				item := testWorkItem()
				item.Status = workflow.WorkItemStatusPending
				return item
			},
			result: func(item workflow.WorkItem) sdkcollector.Result { return completeResult(item, testSDKFact(item)) },
			want: ResultRecord{
				CollectorKind: "repo", Origin: ResultOriginHost, State: sdkcollector.ResultTerminal,
				FailureClass: FailureClassInvalidClaim,
			},
		},
		{
			name: "invalid claim with undeclared kind",
			item: func() workflow.WorkItem {
				item := testWorkItem()
				item.CollectorKind = scope.CollectorKind("someone-elses-kind")
				return item
			},
			result: func(item workflow.WorkItem) sdkcollector.Result { return completeResult(item, testSDKFact(item)) },
			want: ResultRecord{
				CollectorKind: unknownCollectorKind, Origin: ResultOriginHost,
				State: sdkcollector.ResultTerminal, FailureClass: FailureClassInvalidClaim,
			},
		},
		{
			name:      "launch failure",
			runnerErr: errors.New("exec: no such file"),
			result:    func(item workflow.WorkItem) sdkcollector.Result { return sdkcollector.Result{} },
			want: ResultRecord{
				CollectorKind: "repo", Origin: ResultOriginHost, State: sdkcollector.ResultRetryable,
				FailureClass: FailureClassLaunchFailure,
			},
		},
		{
			name: "invalid result",
			result: func(item workflow.WorkItem) sdkcollector.Result {
				result := completeResult(item, testSDKFact(item))
				result.Facts[0].Kind = "dev.example.undeclared"
				return result
			},
			want: ResultRecord{
				CollectorKind: "repo", Origin: ResultOriginHost, State: sdkcollector.ResultTerminal,
				FailureClass: FailureClassInvalidResult,
			},
		},
		{
			name: "identity mismatch",
			result: func(item workflow.WorkItem) sdkcollector.Result {
				result := completeResult(item, testSDKFact(item))
				result.Claim.WorkItemID = "work-someone-else"
				return result
			},
			want: ResultRecord{
				CollectorKind: "repo", Origin: ResultOriginHost, State: sdkcollector.ResultTerminal,
				FailureClass: FailureClassIdentityMismatch,
			},
		},
		{
			name:      "status record failure",
			recordErr: errors.New("recorder down"),
			result:    func(item workflow.WorkItem) sdkcollector.Result { return completeResult(item, testSDKFact(item)) },
			want: ResultRecord{
				CollectorKind: "repo", Origin: ResultOriginHost, State: sdkcollector.ResultRetryable,
				FailureClass: FailureClassStatusRecord,
			},
			wantRecords: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			item := testWorkItem()
			if tc.item != nil {
				item = tc.item()
			}
			recorder := &recordingResultRecorder{recordingStatusRecorder: recordingStatusRecorder{err: tc.recordErr}}
			runner := &recordingRunner{result: tc.result(item), err: tc.runnerErr}
			source := mustNewSource(t, runner, recorder)
			collected, _, _ := source.NextClaimed(context.Background(), item)
			_ = collectFacts(t, collected)

			if len(recorder.results) != 1 {
				t.Fatalf("result records = %d, want exactly 1", len(recorder.results))
			}
			if got := recorder.results[0]; got != tc.want {
				t.Fatalf("result record = %+v, want %+v", got, tc.want)
			}
			if got := len(recorder.statuses); got != tc.wantRecords {
				t.Fatalf("status records = %d, want %d", got, tc.wantRecords)
			}
		})
	}
}

// TestStatusTelemetryRecordsStatusFixture drives a retryable status fixture
// through the production adapter: the instruments move and one log line
// reports the bounded result fields, with no scope, instance, work item,
// generation, or source URL value in either.
func TestStatusTelemetryRecordsStatusFixture(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	logs := &bytes.Buffer{}
	adapter := NewStatusTelemetry(inst, slog.New(slog.NewJSONHandler(logs, nil)))
	item := testWorkItem()
	result := retryableResultWithDelays(item, 30, 120)
	result.Statuses[0].FactCount = 4
	result.Statuses[0].WarningCount = 1
	result.Statuses[1].SourceLatencyMS = 750
	source := mustNewSource(t, &recordingRunner{result: result}, adapter)

	if _, _, err := source.NextClaimed(context.Background(), item); err == nil {
		t.Fatal("NextClaimed() error = nil, want retryable failure")
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	checks := map[string]int64{
		contract.MetricComponentExtensionResults:          1,
		contract.MetricComponentExtensionStatusRecords:    2,
		contract.MetricComponentExtensionReportedFacts:    4,
		contract.MetricComponentExtensionReportedWarnings: 1,
	}
	for name, want := range checks {
		if got := sumWithAttr(rm, name, "collector_kind", "repo"); got != want {
			t.Fatalf("%s{collector_kind=repo} = %d, want %d", name, got, want)
		}
	}
	if got := sumWithAttr(rm, contract.MetricComponentExtensionResults, "failure_class", "rate_limited"); got != 1 {
		t.Fatalf("results{failure_class=rate_limited} = %d, want 1", got)
	}
	if got := gaugeValue(rm, contract.MetricComponentExtensionRetryAfter); got != 120 {
		t.Fatalf("retry-after gauge = %v, want 120", got)
	}
	assertFamilyLabelsBounded(t, rm, item)

	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("log lines = %d, want 1 per claim result: %s", len(lines), logs.String())
	}
	var line map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &line); err != nil {
		t.Fatalf("log line is not JSON: %v", err)
	}
	wantFields := map[string]any{
		"level":                                                  "WARN",
		"collector_kind":                                         "repo",
		"failure_class":                                          "rate_limited",
		contract.LogKeyExtensionStatusOrigin:                     "sdk",
		contract.LogKeyExtensionStatusState:                      "retryable",
		contract.LogKeyExtensionStatusCount:                      float64(2),
		contract.LogKeyExtensionStatusFactCount:                  float64(4),
		contract.LogKeyExtensionStatusRetryAfterSeconds:          float64(120),
		contract.LogKeyExtensionStatusRetryAfterRequestedSeconds: float64(120),
		contract.LogKeyExtensionStatusSourceLatencyMS:            float64(750),
	}
	for key, want := range wantFields {
		if got := line[key]; got != want {
			t.Fatalf("log %s = %v, want %v (line %s)", key, got, want, lines[0])
		}
	}
	for _, leaked := range itemIdentityValues(item) {
		if strings.Contains(lines[0], leaked) {
			t.Fatalf("log line carries identity value %q: %s", leaked, lines[0])
		}
	}
}

// itemIdentityValues lists claim identity and source values that must never
// reach a label or the result log line.
func itemIdentityValues(item workflow.WorkItem) []string {
	return []string{
		item.ScopeID, item.CollectorInstanceID, item.WorkItemID, item.GenerationID,
		item.CurrentClaimID, item.CurrentOwnerID, item.TenantID, item.FairnessKey,
		"git://repo-eshu", "scorecard-record",
	}
}

// assertFamilyLabelsBounded fails when any component-extension data point
// carries a label outside the bounded set or an identity value.
func assertFamilyLabelsBounded(t *testing.T, rm metricdata.ResourceMetrics, item workflow.WorkItem) {
	t.Helper()
	allowed := map[string]bool{"collector_kind": true, "state": true, "status_class": true, "failure_class": true}
	forbidden := map[string]bool{}
	for _, value := range itemIdentityValues(item) {
		forbidden[value] = true
	}
	seen := 0
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if !strings.HasPrefix(m.Name, "eshu_dp_component_extension_") {
				continue
			}
			for _, attrs := range pointAttributeSets(m.Data) {
				seen++
				for _, kv := range attrs {
					if !allowed[string(kv.Key)] {
						t.Fatalf("%s carries label %q, want only collector_kind/state/status_class/failure_class", m.Name, kv.Key)
					}
					if forbidden[kv.Value.String()] {
						t.Fatalf("%s label %s carries identity value %q", m.Name, kv.Key, kv.Value.String())
					}
				}
			}
		}
	}
	if seen == 0 {
		t.Fatal("no component-extension data points recorded")
	}
}

// gaugeValue returns the first point of a float64 gauge, or -1 when absent.
func gaugeValue(rm metricdata.ResourceMetrics, name string) float64 {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if gauge, ok := m.Data.(metricdata.Gauge[float64]); ok && m.Name == name && len(gauge.DataPoints) > 0 {
				return gauge.DataPoints[0].Value
			}
		}
	}
	return -1
}

// pointAttributeSets returns each data point's attributes for the
// aggregation types the component-extension family uses.
func pointAttributeSets(data metricdata.Aggregation) [][]attribute.KeyValue {
	var out [][]attribute.KeyValue
	switch typed := data.(type) {
	case metricdata.Sum[int64]:
		for _, dp := range typed.DataPoints {
			out = append(out, dp.Attributes.ToSlice())
		}
	case metricdata.Histogram[float64]:
		for _, dp := range typed.DataPoints {
			out = append(out, dp.Attributes.ToSlice())
		}
	case metricdata.Gauge[float64]:
		for _, dp := range typed.DataPoints {
			out = append(out, dp.Attributes.ToSlice())
		}
	}
	return out
}
