package cloudruntime

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/correlation/engine"
	"github.com/eshu-hq/eshu/go/internal/correlation/rules"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestRecordEvaluationEmitsOrphanAndUnmanagedCounters(t *testing.T) {
	t.Parallel()

	inst, reader := newCloudRuntimeInstruments(t)
	candidates := BuildCandidates([]AddressedRow{
		{
			ARN:          "arn:aws:lambda:us-east-1:123456789012:function:worker",
			ResourceType: "aws_lambda_function",
			Cloud:        &ResourceRow{ARN: "arn:aws:lambda:us-east-1:123456789012:function:worker"},
		},
		{
			ARN:          "arn:aws:ecs:us-east-1:123456789012:service/prod/api",
			ResourceType: "aws_ecs_service",
			Cloud:        &ResourceRow{ARN: "arn:aws:ecs:us-east-1:123456789012:service/prod/api"},
			State:        &ResourceRow{ARN: "arn:aws:ecs:us-east-1:123456789012:service/prod/api"},
		},
	}, "aws_account:123456789012:us-east-1")

	evaluation, err := engine.Evaluate(rules.AWSCloudRuntimeDriftRulePack(), candidates)
	if err != nil {
		t.Fatalf("Evaluate() error = %v, want nil", err)
	}
	summary := RecordEvaluation(context.Background(), inst, evaluation)
	if summary.OrphanedResources != 1 {
		t.Fatalf("summary.OrphanedResources = %d, want 1", summary.OrphanedResources)
	}
	if summary.UnmanagedResources != 1 {
		t.Fatalf("summary.UnmanagedResources = %d, want 1", summary.UnmanagedResources)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := counterTotal(rm, "eshu_dp_correlation_orphan_detected_total"); got != 1 {
		t.Fatalf("orphan counter = %d, want 1", got)
	}
	if got := counterTotal(rm, "eshu_dp_correlation_unmanaged_detected_total"); got != 1 {
		t.Fatalf("unmanaged counter = %d, want 1", got)
	}
	if got := counterTotal(rm, "eshu_dp_correlation_rule_matches_total"); got != 2 {
		t.Fatalf("rule match counter = %d, want 2", got)
	}
	assertCounterLabelKeys(t, rm, "eshu_dp_correlation_orphan_detected_total", map[string]struct{}{
		telemetry.MetricDimensionPack: {},
		telemetry.MetricDimensionRule: {},
	})
	assertCounterLabelKeys(t, rm, "eshu_dp_correlation_unmanaged_detected_total", map[string]struct{}{
		telemetry.MetricDimensionPack: {},
		telemetry.MetricDimensionRule: {},
	})
}

func TestRecordEvaluationKeepsARNOutOfMetricLabels(t *testing.T) {
	t.Parallel()

	inst, reader := newCloudRuntimeInstruments(t)
	arn := "arn:aws:lambda:us-east-1:123456789012:function:worker"
	evaluation, err := engine.Evaluate(rules.AWSCloudRuntimeDriftRulePack(), BuildCandidates([]AddressedRow{{
		ARN:          arn,
		ResourceType: "aws_lambda_function",
		Cloud:        &ResourceRow{ARN: arn},
	}}, "aws_account:123456789012:us-east-1"))
	if err != nil {
		t.Fatalf("Evaluate() error = %v, want nil", err)
	}
	RecordEvaluation(context.Background(), inst, evaluation)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for _, sm := range rm.ScopeMetrics {
		for _, metric := range sm.Metrics {
			sum, ok := metric.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				iter := dp.Attributes.Iter()
				for iter.Next() {
					if iter.Attribute().Value.AsString() == arn {
						t.Fatalf("metric %q has ARN as a label value", metric.Name)
					}
				}
			}
		}
	}
}

func newCloudRuntimeInstruments(t *testing.T) (*telemetry.Instruments, sdkmetric.Reader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return inst, reader
}

func counterTotal(rm metricdata.ResourceMetrics, name string) int64 {
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, metric := range sm.Metrics {
			if metric.Name != name {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				total += dp.Value
			}
		}
	}
	return total
}

func assertCounterLabelKeys(t *testing.T, rm metricdata.ResourceMetrics, name string, allowed map[string]struct{}) {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, metric := range sm.Metrics {
			if metric.Name != name {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				iter := dp.Attributes.Iter()
				for iter.Next() {
					key := string(iter.Attribute().Key)
					if _, ok := allowed[key]; !ok {
						t.Fatalf("metric %q label key %q is not allowed", name, key)
					}
				}
			}
		}
	}
}
