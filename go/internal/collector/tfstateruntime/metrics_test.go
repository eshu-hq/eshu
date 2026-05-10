package tfstateruntime_test

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/collector/tfstateruntime"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestClaimedSourceRecordsTerraformStateReaderMetrics(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 10, 16, 0, 0, 0, time.UTC)
	key, err := redact.NewKey([]byte("runtime-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v, want nil", err)
	}
	stateKey := terraformstate.StateKey{
		BackendKind: terraformstate.BackendS3,
		Locator:     "s3://tfstate-prod/services/api/terraform.tfstate",
	}
	state := `{
		"serial": 17,
		"lineage": "lineage-123",
		"resources": [{
			"mode": "managed",
			"type": "aws_db_instance",
			"name": "primary",
			"instances": [{
				"attributes": {
					"id": "db-1",
					"password": "secret"
				}
			}]
		}]
	}`
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("tfstate-runtime-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	source := tfstateruntime.ClaimedSource{
		Resolver: terraformstate.DiscoveryResolver{
			Config: terraformstate.DiscoveryConfig{
				Seeds: []terraformstate.DiscoverySeed{{
					Kind:   terraformstate.BackendS3,
					Bucket: "tfstate-prod",
					Key:    "services/api/terraform.tfstate",
					Region: "us-east-1",
				}},
			},
		},
		SourceFactory: &fakeFactory{
			source: &fakeStateSource{
				key:        stateKey,
				state:      state,
				observedAt: observedAt,
			},
		},
		RedactionKey: key,
		Clock:        func() time.Time { return observedAt },
		Instruments:  instruments,
	}
	scopeValue, generationValue := tfstateScopeAndGenerationForTest(t, stateKey, observedAt)

	_, ok, err := source.NextClaimed(context.Background(), workflow.WorkItem{
		WorkItemID:          "tfstate-work-metrics",
		RunID:               "run-1",
		CollectorKind:       scope.CollectorTerraformState,
		CollectorInstanceID: "collector-tfstate-primary",
		SourceSystem:        string(scope.CollectorTerraformState),
		ScopeID:             scopeValue.ScopeID,
		AcceptanceUnitID:    "platform-infra",
		SourceRunID:         generationValue.GenerationID,
		GenerationID:        generationValue.GenerationID,
		Status:              workflow.WorkItemStatusClaimed,
		AttemptCount:        1,
		CurrentFencingToken: 42,
		CreatedAt:           observedAt,
		UpdatedAt:           observedAt,
	})
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	assertInt64MetricValue(t, rm, "eshu_dp_tfstate_snapshots_observed_total", 1)
	assertInt64MetricValue(t, rm, "eshu_dp_tfstate_resources_emitted_total", 1)
	assertInt64MetricValue(t, rm, "eshu_dp_tfstate_redactions_applied_total", 2)
	assertInt64HistogramCount(t, rm, "eshu_dp_tfstate_snapshot_bytes", 1)
	assertFloat64HistogramCount(t, rm, "eshu_dp_tfstate_parse_duration_seconds", 1)
	assertMetricMissingAttribute(t, rm, "eshu_dp_tfstate_snapshots_observed_total", "scope_id")
}

func TestClaimedSourceRecordsS3NotModifiedMetric(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 10, 16, 30, 0, 0, time.UTC)
	key, err := redact.NewKey([]byte("runtime-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v, want nil", err)
	}
	stateKey := terraformstate.StateKey{
		BackendKind: terraformstate.BackendS3,
		Locator:     "s3://tfstate-prod/services/api/terraform.tfstate",
	}
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("tfstate-runtime-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	source := tfstateruntime.ClaimedSource{
		Resolver: terraformstate.DiscoveryResolver{
			Config: terraformstate.DiscoveryConfig{
				Seeds: []terraformstate.DiscoverySeed{{
					Kind:   terraformstate.BackendS3,
					Bucket: "tfstate-prod",
					Key:    "services/api/terraform.tfstate",
					Region: "us-east-1",
				}},
			},
		},
		SourceFactory: &fakeFactory{source: &fakeStateSource{key: stateKey, openErr: terraformstate.ErrStateNotModified}},
		RedactionKey:  key,
		Clock:         func() time.Time { return observedAt },
		Instruments:   instruments,
	}
	scopeValue, generationValue := tfstateScopeAndGenerationForTest(t, stateKey, observedAt)

	_, ok, err := source.NextClaimed(context.Background(), workflow.WorkItem{
		WorkItemID:          "tfstate-work-not-modified-metrics",
		RunID:               "run-1",
		CollectorKind:       scope.CollectorTerraformState,
		CollectorInstanceID: "collector-tfstate-primary",
		SourceSystem:        string(scope.CollectorTerraformState),
		ScopeID:             scopeValue.ScopeID,
		AcceptanceUnitID:    "platform-infra",
		SourceRunID:         generationValue.GenerationID,
		GenerationID:        generationValue.GenerationID,
		Status:              workflow.WorkItemStatusClaimed,
		AttemptCount:        1,
		CurrentFencingToken: 42,
		CreatedAt:           observedAt,
		UpdatedAt:           observedAt,
	})
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	assertInt64MetricValue(t, rm, "eshu_dp_tfstate_s3_conditional_get_not_modified_total", 1)
	assertInt64MetricValue(t, rm, "eshu_dp_tfstate_snapshots_observed_total", 1)
}

func TestClaimedSourceRecordsErrorMetricWhenIdentityReadFails(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 10, 16, 45, 0, 0, time.UTC)
	key, err := redact.NewKey([]byte("runtime-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v, want nil", err)
	}
	stateKey := terraformstate.StateKey{
		BackendKind: terraformstate.BackendS3,
		Locator:     "s3://tfstate-prod/services/api/terraform.tfstate",
	}
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("tfstate-runtime-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	source := tfstateruntime.ClaimedSource{
		Resolver: terraformstate.DiscoveryResolver{
			Config: terraformstate.DiscoveryConfig{
				Seeds: []terraformstate.DiscoverySeed{{
					Kind:   terraformstate.BackendS3,
					Bucket: "tfstate-prod",
					Key:    "services/api/terraform.tfstate",
					Region: "us-east-1",
				}},
			},
		},
		SourceFactory: &fakeFactory{source: &fakeStateSource{key: stateKey, state: `{`}},
		RedactionKey:  key,
		Clock:         func() time.Time { return observedAt },
		Instruments:   instruments,
	}
	scopeValue, generationValue := tfstateScopeAndGenerationForTest(t, stateKey, observedAt)

	_, ok, err := source.NextClaimed(context.Background(), workflow.WorkItem{
		WorkItemID:          "tfstate-work-identity-error-metrics",
		RunID:               "run-1",
		CollectorKind:       scope.CollectorTerraformState,
		CollectorInstanceID: "collector-tfstate-primary",
		SourceSystem:        string(scope.CollectorTerraformState),
		ScopeID:             scopeValue.ScopeID,
		AcceptanceUnitID:    "platform-infra",
		SourceRunID:         generationValue.GenerationID,
		GenerationID:        generationValue.GenerationID,
		Status:              workflow.WorkItemStatusClaimed,
		AttemptCount:        1,
		CurrentFencingToken: 42,
		CreatedAt:           observedAt,
		UpdatedAt:           observedAt,
	})
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want identity read failure")
	}
	if ok {
		t.Fatal("NextClaimed() ok = true, want false")
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	assertInt64MetricValue(t, rm, "eshu_dp_tfstate_snapshots_observed_total", 1)
}

func tfstateScopeAndGenerationForTest(
	t *testing.T,
	stateKey terraformstate.StateKey,
	observedAt time.Time,
) (scope.IngestionScope, scope.ScopeGeneration) {
	t.Helper()
	scopeValue, err := scope.NewTerraformStateSnapshotScope("", string(terraformstate.BackendS3), stateKey.Locator, nil)
	if err != nil {
		t.Fatalf("NewTerraformStateSnapshotScope() error = %v, want nil", err)
	}
	generationValue, err := scope.NewTerraformStateSnapshotGeneration(scopeValue.ScopeID, 17, "lineage-123", observedAt)
	if err != nil {
		t.Fatalf("NewTerraformStateSnapshotGeneration() error = %v, want nil", err)
	}
	return scopeValue, generationValue
}

func assertInt64MetricValue(t *testing.T, rm metricdata.ResourceMetrics, metricName string, want int64) {
	t.Helper()
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != metricName {
				continue
			}
			sum, ok := metricRecord.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Sum[int64]", metricName, metricRecord.Data)
			}
			var got int64
			for _, point := range sum.DataPoints {
				got += point.Value
			}
			if got != want {
				t.Fatalf("%s = %d, want %d", metricName, got, want)
			}
			return
		}
	}
	t.Fatalf("metric %s not found", metricName)
}

func assertInt64HistogramCount(t *testing.T, rm metricdata.ResourceMetrics, metricName string, want uint64) {
	t.Helper()
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != metricName {
				continue
			}
			histogram, ok := metricRecord.Data.(metricdata.Histogram[int64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Histogram[int64]", metricName, metricRecord.Data)
			}
			var got uint64
			for _, point := range histogram.DataPoints {
				got += point.Count
			}
			if got != want {
				t.Fatalf("%s count = %d, want %d", metricName, got, want)
			}
			return
		}
	}
	t.Fatalf("metric %s not found", metricName)
}

func assertFloat64HistogramCount(t *testing.T, rm metricdata.ResourceMetrics, metricName string, want uint64) {
	t.Helper()
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != metricName {
				continue
			}
			histogram, ok := metricRecord.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Histogram[float64]", metricName, metricRecord.Data)
			}
			var got uint64
			for _, point := range histogram.DataPoints {
				got += point.Count
			}
			if got != want {
				t.Fatalf("%s count = %d, want %d", metricName, got, want)
			}
			return
		}
	}
	t.Fatalf("metric %s not found", metricName)
}

func assertMetricMissingAttribute(t *testing.T, rm metricdata.ResourceMetrics, metricName string, key string) {
	t.Helper()
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != metricName {
				continue
			}
			sum, ok := metricRecord.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Sum[int64]", metricName, metricRecord.Data)
			}
			for _, point := range sum.DataPoints {
				for _, attr := range point.Attributes.ToSlice() {
					if string(attr.Key) == key {
						t.Fatalf("%s has unexpected attribute %q", metricName, key)
					}
				}
			}
			return
		}
	}
	t.Fatalf("metric %s not found", metricName)
}
