package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func TestNewInstrumentsNoError(t *testing.T) {
	meter := noop.NewMeterProvider().Meter("test")
	inst, err := NewInstruments(meter)

	require.NoError(t, err, "NewInstruments should succeed with noop meter")
	require.NotNil(t, inst, "Instruments should not be nil")

	// Verify all counter fields are non-nil
	assert.NotNil(t, inst.FactsEmitted, "FactsEmitted counter should be registered")
	assert.NotNil(t, inst.FactsCommitted, "FactsCommitted counter should be registered")
	assert.NotNil(t, inst.ProjectionsCompleted, "ProjectionsCompleted counter should be registered")
	assert.NotNil(t, inst.ReducerIntentsEnqueued, "ReducerIntentsEnqueued counter should be registered")
	assert.NotNil(t, inst.ReducerExecutions, "ReducerExecutions counter should be registered")
	assert.NotNil(t, inst.CanonicalWrites, "CanonicalWrites counter should be registered")
	assert.NotNil(t, inst.SharedProjectionCycles, "SharedProjectionCycles counter should be registered")
	assert.NotNil(t, inst.SharedAcceptanceUpserts, "SharedAcceptanceUpserts counter should be registered")
	assert.NotNil(t, inst.SharedAcceptanceLookupErrors, "SharedAcceptanceLookupErrors counter should be registered")
	assert.NotNil(t, inst.SharedProjectionStaleIntents, "SharedProjectionStaleIntents counter should be registered")
	assert.NotNil(t, inst.DocumentationEntityMentions, "DocumentationEntityMentions counter should be registered")
	assert.NotNil(t, inst.DocumentationClaimCandidates, "DocumentationClaimCandidates counter should be registered")
	assert.NotNil(t, inst.DocumentationClaimsSuppressed, "DocumentationClaimsSuppressed counter should be registered")
	assert.NotNil(t, inst.DocumentationDriftFindings, "DocumentationDriftFindings counter should be registered")
	assert.NotNil(t, inst.IaCReachabilityRows, "IaCReachabilityRows counter should be registered")
	assert.NotNil(t, inst.TerraformStateSnapshotsObserved, "TerraformStateSnapshotsObserved counter should be registered")
	assert.NotNil(t, inst.TerraformStateResourcesEmitted, "TerraformStateResourcesEmitted counter should be registered")
	assert.NotNil(t, inst.TerraformStateOutputsEmitted, "TerraformStateOutputsEmitted counter should be registered")
	assert.NotNil(t, inst.TerraformStateModulesEmitted, "TerraformStateModulesEmitted counter should be registered")
	assert.NotNil(t, inst.TerraformStateWarningsEmitted, "TerraformStateWarningsEmitted counter should be registered")
	assert.NotNil(t, inst.TerraformStateRedactionsApplied, "TerraformStateRedactionsApplied counter should be registered")
	assert.NotNil(t, inst.TerraformStateS3ConditionalGetNotModified, "TerraformStateS3ConditionalGetNotModified counter should be registered")
	assert.NotNil(t, inst.CorrelationDriftIntentsEnqueued, "CorrelationDriftIntentsEnqueued counter should be registered")

	// Verify all histogram fields are non-nil
	assert.NotNil(t, inst.CollectorObserveDuration, "CollectorObserveDuration histogram should be registered")
	assert.NotNil(t, inst.TerraformStateClaimWaitDuration, "TerraformStateClaimWaitDuration histogram should be registered")
	assert.NotNil(t, inst.ScopeAssignDuration, "ScopeAssignDuration histogram should be registered")
	assert.NotNil(t, inst.FactEmitDuration, "FactEmitDuration histogram should be registered")
	assert.NotNil(t, inst.ProjectorRunDuration, "ProjectorRunDuration histogram should be registered")
	assert.NotNil(t, inst.ProjectorStageDuration, "ProjectorStageDuration histogram should be registered")
	assert.NotNil(t, inst.ReducerRunDuration, "ReducerRunDuration histogram should be registered")
	assert.NotNil(t, inst.CanonicalWriteDuration, "CanonicalWriteDuration histogram should be registered")
	assert.NotNil(t, inst.QueueClaimDuration, "QueueClaimDuration histogram should be registered")
	assert.NotNil(t, inst.BatchClaimSize, "BatchClaimSize histogram should be registered")
	assert.NotNil(t, inst.PostgresQueryDuration, "PostgresQueryDuration histogram should be registered")
	assert.NotNil(t, inst.Neo4jQueryDuration, "Neo4jQueryDuration histogram should be registered")
	assert.NotNil(t, inst.SharedEdgeWriteGroups, "SharedEdgeWriteGroups counter should be registered")
	assert.NotNil(t, inst.SharedEdgeWriteGroupDuration, "SharedEdgeWriteGroupDuration histogram should be registered")
	assert.NotNil(t, inst.SharedEdgeWriteGroupStatementCount, "SharedEdgeWriteGroupStatementCount histogram should be registered")
	assert.NotNil(t, inst.CodeCallEdgeBatches, "CodeCallEdgeBatches counter should be registered")
	assert.NotNil(t, inst.CodeCallEdgeDuration, "CodeCallEdgeDuration histogram should be registered")
	assert.NotNil(t, inst.SharedAcceptanceUpsertDuration, "SharedAcceptanceUpsertDuration histogram should be registered")
	assert.NotNil(t, inst.SharedAcceptanceLookupDuration, "SharedAcceptanceLookupDuration histogram should be registered")
	assert.NotNil(t, inst.SharedAcceptancePrefetchSize, "SharedAcceptancePrefetchSize histogram should be registered")
	assert.NotNil(t, inst.SharedProjectionIntentWaitDuration, "SharedProjectionIntentWaitDuration histogram should be registered")
	assert.NotNil(t, inst.SharedProjectionProcessingDuration, "SharedProjectionProcessingDuration histogram should be registered")
	assert.NotNil(t, inst.SharedProjectionStepDuration, "SharedProjectionStepDuration histogram should be registered")
	assert.NotNil(t, inst.DocumentationDriftGenerationDuration, "DocumentationDriftGenerationDuration histogram should be registered")
	assert.NotNil(t, inst.IaCReachabilityMaterializationDuration, "IaCReachabilityMaterializationDuration histogram should be registered")
	assert.NotNil(t, inst.TerraformStateSnapshotBytes, "TerraformStateSnapshotBytes histogram should be registered")
	assert.NotNil(t, inst.TerraformStateParseDuration, "TerraformStateParseDuration histogram should be registered")
}

func TestNewInstrumentsNilMeterError(t *testing.T) {
	inst, err := NewInstruments(nil)

	require.Error(t, err, "NewInstruments should fail with nil meter")
	assert.Nil(t, inst, "Instruments should be nil on error")
	assert.Contains(t, err.Error(), "meter is required", "Error should mention meter requirement")
}

func TestAttrHelpers(t *testing.T) {
	tests := []struct {
		name     string
		attrFunc func(string) string
		wantKey  string
	}{
		{
			name:     "AttrScopeID",
			attrFunc: func(v string) string { return string(AttrScopeID(v).Key) },
			wantKey:  MetricDimensionScopeID,
		},
		{
			name:     "AttrScopeKind",
			attrFunc: func(v string) string { return string(AttrScopeKind(v).Key) },
			wantKey:  MetricDimensionScopeKind,
		},
		{
			name:     "AttrSource",
			attrFunc: func(v string) string { return string(AttrSource(v).Key) },
			wantKey:  MetricDimensionSource,
		},
		{
			name:     "AttrSourceSystem",
			attrFunc: func(v string) string { return string(AttrSourceSystem(v).Key) },
			wantKey:  MetricDimensionSourceSystem,
		},
		{
			name:     "AttrGenerationID",
			attrFunc: func(v string) string { return string(AttrGenerationID(v).Key) },
			wantKey:  MetricDimensionGenerationID,
		},
		{
			name:     "AttrCollectorKind",
			attrFunc: func(v string) string { return string(AttrCollectorKind(v).Key) },
			wantKey:  MetricDimensionCollectorKind,
		},
		{
			name:     "AttrDomain",
			attrFunc: func(v string) string { return string(AttrDomain(v).Key) },
			wantKey:  MetricDimensionDomain,
		},
		{
			name:     "AttrPartitionKey",
			attrFunc: func(v string) string { return string(AttrPartitionKey(v).Key) },
			wantKey:  MetricDimensionPartitionKey,
		},
		{
			name:     "AttrRunner",
			attrFunc: func(v string) string { return string(AttrRunner(v).Key) },
			wantKey:  MetricDimensionRunner,
		},
		{
			name:     "AttrLookupResult",
			attrFunc: func(v string) string { return string(AttrLookupResult(v).Key) },
			wantKey:  MetricDimensionLookupResult,
		},
		{
			name:     "AttrErrorType",
			attrFunc: func(v string) string { return string(AttrErrorType(v).Key) },
			wantKey:  MetricDimensionErrorType,
		},
		{
			name:     "AttrOutcome",
			attrFunc: func(v string) string { return string(AttrOutcome(v).Key) },
			wantKey:  MetricDimensionOutcome,
		},
		{
			name:     "AttrBackendKind",
			attrFunc: func(v string) string { return string(AttrBackendKind(v).Key) },
			wantKey:  MetricDimensionBackendKind,
		},
		{
			name:     "AttrResult",
			attrFunc: func(v string) string { return string(AttrResult(v).Key) },
			wantKey:  MetricDimensionResult,
		},
		{
			name:     "AttrReason",
			attrFunc: func(v string) string { return string(AttrReason(v).Key) },
			wantKey:  MetricDimensionReason,
		},
		{
			name:     "AttrSafeLocatorHash",
			attrFunc: func(v string) string { return string(AttrSafeLocatorHash(v).Key) },
			wantKey:  MetricDimensionSafeLocatorHash,
		},
		{
			name:     "AttrWarningKind",
			attrFunc: func(v string) string { return string(AttrWarningKind(v).Key) },
			wantKey:  MetricDimensionWarningKind,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotKey := tt.attrFunc("test-value")
			assert.Equal(t, tt.wantKey, gotKey,
				"Attribute key should match contract constant")
		})
	}
}

func TestRegisterObservableGauges_NilInstruments(t *testing.T) {
	meter := sdkmetric.NewMeterProvider().Meter("test")
	err := RegisterObservableGauges(nil, meter, nil, nil)
	if err == nil {
		t.Fatal("expected error for nil instruments")
	}
}

func TestRegisterObservableGauges_NilMeter(t *testing.T) {
	inst := &Instruments{}
	err := RegisterObservableGauges(inst, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for nil meter")
	}
}

func TestRegisterObservableGauges_NilObservers(t *testing.T) {
	meter := sdkmetric.NewMeterProvider().Meter("test")
	inst := &Instruments{}
	err := RegisterObservableGauges(inst, meter, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error with nil observers: %v", err)
	}
}

func TestRegisterObservableGauges_WithObservers(t *testing.T) {
	meter := sdkmetric.NewMeterProvider().Meter("test")
	inst := &Instruments{}

	queueObs := &fakeQueueObserver{
		depths: map[string]map[string]int64{
			"projector": {"pending": 5, "in_flight": 2},
		},
		ages: map[string]float64{
			"projector": 30.5,
		},
	}
	workerObs := &fakeWorkerObserver{
		counts: map[string]int64{
			"collector": 3,
			"projector": 2,
		},
	}

	err := RegisterObservableGauges(inst, meter, queueObs, workerObs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inst.QueueDepth == nil {
		t.Error("expected QueueDepth gauge to be set")
	}
	if inst.QueueOldestAge == nil {
		t.Error("expected QueueOldestAge gauge to be set")
	}
	if inst.WorkerPoolActive == nil {
		t.Error("expected WorkerPoolActive gauge to be set")
	}
}

func TestRegisterAcceptanceObservableGauges_NilInputs(t *testing.T) {
	meter := sdkmetric.NewMeterProvider().Meter("test")
	inst := &Instruments{}

	if err := RegisterAcceptanceObservableGauges(nil, meter, nil); err == nil {
		t.Fatal("expected error for nil instruments")
	}
	if err := RegisterAcceptanceObservableGauges(inst, nil, nil); err == nil {
		t.Fatal("expected error for nil meter")
	}
}

func TestRegisterAcceptanceObservableGauges_WithObserver(t *testing.T) {
	meter := sdkmetric.NewMeterProvider().Meter("test")
	inst := &Instruments{}
	observer := &fakeAcceptanceObserver{rows: 42}

	if err := RegisterAcceptanceObservableGauges(inst, meter, observer); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inst.SharedAcceptanceRows == nil {
		t.Fatal("expected SharedAcceptanceRows gauge to be set")
	}
}

type fakeQueueObserver struct {
	depths map[string]map[string]int64
	ages   map[string]float64
}

func (f *fakeQueueObserver) QueueDepths(_ context.Context) (map[string]map[string]int64, error) {
	return f.depths, nil
}

func (f *fakeQueueObserver) QueueOldestAge(_ context.Context) (map[string]float64, error) {
	return f.ages, nil
}

type fakeWorkerObserver struct {
	counts map[string]int64
}

func (f *fakeWorkerObserver) ActiveWorkers(_ context.Context) (map[string]int64, error) {
	return f.counts, nil
}

type fakeAcceptanceObserver struct {
	rows int64
}

func (f *fakeAcceptanceObserver) AcceptanceRowCount(_ context.Context) (int64, error) {
	return f.rows, nil
}
