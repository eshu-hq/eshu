package tfstateruntime_test

import (
	"context"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/collector/tfstateruntime"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestClaimedSourceRecordsOutputModuleAndWarningCounters proves that the
// streaming parser surfaces output, module observation, and warning counts back
// to the collector runtime, which records them on the contract counters with
// safe_locator_hash and warning_kind labels.
func TestClaimedSourceRecordsOutputModuleAndWarningCounters(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 10, 17, 0, 0, 0, time.UTC)
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
		"outputs": {
			"primary_endpoint": {"sensitive": false, "value": "https://api.example.com"},
			"db_password": {"sensitive": true, "value": "redacted-secret"}
		},
		"resources": [
			{
				"mode": "managed",
				"type": "aws_db_instance",
				"name": "primary",
				"module": "module.db",
				"instances": [{"attributes": {"id": "db-1"}}]
			},
			{
				"mode": "managed",
				"type": "aws_s3_bucket",
				"name": "logs",
				"module": "module.logs",
				"instances": [{"attributes": {"id": "bucket-1"}}]
			}
		]
	}`
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("tfstate-runtime-output-test"))
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
		WorkItemID:          "tfstate-output-counter-test",
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

	safeHash := scopeValue.Metadata["locator_hash"]
	if safeHash == "" {
		t.Fatal("scope metadata missing locator_hash")
	}

	assertInt64MetricValue(t, rm, "eshu_dp_tfstate_outputs_emitted_total", 2)
	assertInt64MetricValue(t, rm, "eshu_dp_tfstate_modules_emitted_total", 2)
	assertCounterHasLabel(t, rm, "eshu_dp_tfstate_outputs_emitted_total", "safe_locator_hash", safeHash)
	assertCounterHasLabel(t, rm, "eshu_dp_tfstate_modules_emitted_total", "safe_locator_hash", safeHash)
}

// TestClaimedSourceRecordsTooLargeWarningCounter proves that the state-too-large
// path increments the warning counter with warning_kind=state_too_large even
// though it bypasses the streaming parser.
func TestClaimedSourceRecordsTooLargeWarningCounter(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 10, 17, 30, 0, 0, time.UTC)
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
	instruments, err := telemetry.NewInstruments(provider.Meter("tfstate-runtime-warning-test"))
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
		SourceFactory: &fakeFactory{source: &fakeStateSource{
			key:     stateKey,
			openErr: terraformstate.ErrStateTooLarge,
		}},
		RedactionKey: key,
		Clock:        func() time.Time { return observedAt },
		Instruments:  instruments,
	}
	scopeValue, generationValue := tfstateScopeAndGenerationForTest(t, stateKey, observedAt)

	_, ok, err := source.NextClaimed(context.Background(), workflow.WorkItem{
		WorkItemID:          "tfstate-too-large-counter-test",
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
	assertInt64MetricValue(t, rm, "eshu_dp_tfstate_warnings_emitted_total", 1)
	assertCounterHasLabel(t, rm, "eshu_dp_tfstate_warnings_emitted_total", "warning_kind", "state_too_large")
}

func assertCounterHasLabel(t *testing.T, rm metricdata.ResourceMetrics, metricName, key, value string) {
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
					if string(attr.Key) == key && attr.Value.AsString() == value {
						return
					}
				}
			}
			t.Fatalf("%s missing label %q=%q", metricName, key, value)
		}
	}
	t.Fatalf("metric %s not found", metricName)
}
