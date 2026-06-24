// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pagerduty

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestClaimedSourceRecordsLiveConfigTelemetry(t *testing.T) {
	t.Parallel()

	now := testObservedAt()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	client := staticConfigEvidenceClient{
		result: CollectionResult{ObservedAt: now},
		configResult: ConfigCollectionResult{
			Services:     []ConfigService{{ID: "SVC1", MatchState: ConfigMatchStateNotCompared}},
			Integrations: []ConfigIntegration{{ID: "INT1", ServiceID: "SVC1", DriftReason: "manually_created"}},
			Warnings: []ConfigWarning{{
				ResourceClass: ConfigResourceClassServiceIntegration,
				ResourceID:    "INT2",
				Reason:        ConfigWarningPermissionHidden,
			}},
			ObservedAt: now,
			Partial:    true,
			Redactions: 3,
		},
	}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "pagerduty-primary",
		Targets: []TargetConfig{{
			Provider:                ProviderPagerDuty,
			ScopeID:                 "pagerduty:account:example",
			AccountID:               "example",
			Token:                   "pd-token",
			IncidentLookback:        time.Hour,
			ConfigValidationEnabled: true,
			ConfigResourceLimit:     10,
		}},
		ClientFactory: func(TargetConfig) (EvidenceClient, error) { return client, nil },
		Now:           func() time.Time { return now },
		Instruments:   instruments,
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	collected, ok, err := source.NextClaimed(context.Background(), testPagerDutyWorkItem(now))
	if err != nil || !ok {
		t.Fatalf("NextClaimed() = ok %v err %v, want ok true nil", ok, err)
	}
	_ = drainFacts(collected.Facts)
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}

	assertPagerDutyCounter(t, rm, "eshu_dp_pagerduty_config_resources_observed_total", 2)
	assertPagerDutyCounter(t, rm, "eshu_dp_pagerduty_config_drift_candidates_total", 1)
	assertPagerDutyCounter(t, rm, "eshu_dp_pagerduty_config_partial_failures_total", 1)
	assertPagerDutyCounter(t, rm, "eshu_dp_pagerduty_config_redactions_total", 3)
	assertPagerDutyCounterWithAttribute(
		t,
		rm,
		"eshu_dp_pagerduty_provider_requests_total",
		attribute.String(telemetry.MetricDimensionStatusClass, "partial"),
		1,
	)
}

func TestClaimedSourceRecordsRelatedChangeWarningAsPartialTelemetry(t *testing.T) {
	t.Parallel()

	now := testObservedAt()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	client := staticConfigEvidenceClient{result: CollectionResult{
		Incidents: []Incident{testIncident("P123")},
		Warnings: []ConfigWarning{{
			ResourceClass: ConfigResourceClassRelatedChangeEvent,
			ResourceID:    "P123",
			Reason:        ConfigWarningPermissionHidden,
		}},
		ObservedAt: now,
	}}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "pagerduty-primary",
		Targets: []TargetConfig{{
			Provider:         ProviderPagerDuty,
			ScopeID:          "pagerduty:account:example",
			AccountID:        "example",
			Token:            "pd-token",
			IncidentLookback: time.Hour,
		}},
		ClientFactory: func(TargetConfig) (EvidenceClient, error) { return client, nil },
		Now:           func() time.Time { return now },
		Instruments:   instruments,
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	collected, ok, err := source.NextClaimed(context.Background(), testPagerDutyWorkItem(now))
	if err != nil || !ok {
		t.Fatalf("NextClaimed() = ok %v err %v, want ok true nil", ok, err)
	}
	_ = drainFacts(collected.Facts)
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}

	assertPagerDutyCounterWithAttribute(
		t,
		rm,
		"eshu_dp_pagerduty_provider_requests_total",
		attribute.String(telemetry.MetricDimensionStatusClass, "partial"),
		1,
	)
}

func assertPagerDutyCounter(t *testing.T, rm metricdata.ResourceMetrics, name string, want int64) {
	t.Helper()
	got := int64(0)
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != name {
				continue
			}
			sum, ok := metricRecord.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data = %T, want metricdata.Sum[int64]", name, metricRecord.Data)
			}
			for _, point := range sum.DataPoints {
				got += point.Value
			}
		}
	}
	if got != want {
		t.Fatalf("%s total = %d, want %d", name, got, want)
	}
}

func assertPagerDutyCounterWithAttribute(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	name string,
	wantAttr attribute.KeyValue,
	want int64,
) {
	t.Helper()
	got := int64(0)
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != name {
				continue
			}
			sum, ok := metricRecord.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data = %T, want metricdata.Sum[int64]", name, metricRecord.Data)
			}
			for _, point := range sum.DataPoints {
				if hasAttribute(point.Attributes.ToSlice(), wantAttr) {
					got += point.Value
				}
			}
		}
	}
	if got != want {
		t.Fatalf("%s{%s=%s} total = %d, want %d", name, wantAttr.Key, wantAttr.Value.AsString(), got, want)
	}
}

func hasAttribute(attrs []attribute.KeyValue, want attribute.KeyValue) bool {
	for _, attr := range attrs {
		if attr.Key == want.Key && attr.Value.AsString() == want.Value.AsString() {
			return true
		}
	}
	return false
}
