// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type fakeSharedIntentReader struct {
	mu      sync.Mutex
	intents []SharedProjectionIntentRow
	marked  []string
}

func (f *fakeSharedIntentReader) ListPendingDomainIntents(_ context.Context, domain string, limit int) ([]SharedProjectionIntentRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var result []SharedProjectionIntentRow
	for _, row := range f.intents {
		if row.ProjectionDomain == domain && row.CompletedAt == nil {
			result = append(result, row)
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func (f *fakeSharedIntentReader) MarkIntentsCompleted(_ context.Context, intentIDs []string, completedAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.marked = append(f.marked, intentIDs...)
	idSet := make(map[string]struct{}, len(intentIDs))
	for _, id := range intentIDs {
		idSet[id] = struct{}{}
	}
	for i := range f.intents {
		if _, ok := idSet[f.intents[i].IntentID]; ok {
			t := completedAt
			f.intents[i].CompletedAt = &t
		}
	}
	return nil
}

type fakeLeaseManager struct {
	mu      sync.Mutex
	claims  int
	granted bool
}

func (f *fakeLeaseManager) ClaimPartitionLease(_ context.Context, _ string, _, _ int, _ string, _ time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claims++
	return f.granted, nil
}

func (f *fakeLeaseManager) ReleasePartitionLease(_ context.Context, _ string, _, _ int, _ string) error {
	return nil
}

type fakeEdgeWriter struct {
	mu        sync.Mutex
	writes    int
	retracts  int
	writeRows []SharedProjectionIntentRow
}

func (f *fakeEdgeWriter) WriteEdges(_ context.Context, _ string, rows []SharedProjectionIntentRow, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes++
	f.writeRows = append(f.writeRows, rows...)
	return nil
}

func (f *fakeEdgeWriter) RetractEdges(_ context.Context, _ string, rows []SharedProjectionIntentRow, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retracts++
	return nil
}

func TestSharedProjectionRunnerConfigDefaults(t *testing.T) {
	t.Parallel()

	cfg := SharedProjectionRunnerConfig{}
	if got := cfg.partitionCount(); got != defaultPartitionCount {
		t.Fatalf("partitionCount() = %d, want %d", got, defaultPartitionCount)
	}
	if got := cfg.pollInterval(); got != defaultSharedPollInterval {
		t.Fatalf("pollInterval() = %v, want %v", got, defaultSharedPollInterval)
	}
	if got := cfg.leaseTTL(); got != defaultLeaseTTL {
		t.Fatalf("leaseTTL() = %v, want %v", got, defaultLeaseTTL)
	}
	if got := cfg.batchLimit(); got != defaultBatchLimit {
		t.Fatalf("batchLimit() = %d, want %d", got, defaultBatchLimit)
	}
}

func TestSharedProjectionRunnerStopsOnCancelledContext(t *testing.T) {
	t.Parallel()

	runner := SharedProjectionRunner{
		IntentReader: &fakeSharedIntentReader{},
		LeaseManager: &fakeLeaseManager{granted: false},
		EdgeWriter:   &fakeEdgeWriter{},
		AcceptedGen:  acceptedGenerationFixed("gen-1", true),
		Config: SharedProjectionRunnerConfig{
			PollInterval: 10 * time.Millisecond,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := runner.Run(ctx)
	if err != nil {
		t.Fatalf("Run() error = %v, want nil on cancelled context", err)
	}
}

func TestSharedProjectionRunnerProcessesPendingIntents(t *testing.T) {
	t.Parallel()

	reader := &fakeSharedIntentReader{
		intents: []SharedProjectionIntentRow{
			{
				IntentID:         "intent-1",
				ProjectionDomain: DomainPlatformInfra,
				PartitionKey:     "platform:eks-prod",
				ScopeID:          "scope-a",
				AcceptanceUnitID: "repo-a",
				RepositoryID:     "repo-a",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				Payload:          map[string]any{"action": "upsert", "repo_id": "repo-a", "platform_id": "p1"},
				CreatedAt:        time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC),
			},
		},
	}
	leaseManager := &fakeLeaseManager{granted: true}
	edgeWriter := &fakeEdgeWriter{}

	runner := SharedProjectionRunner{
		IntentReader: reader,
		LeaseManager: leaseManager,
		EdgeWriter:   edgeWriter,
		AcceptedGen:  acceptedGenerationFixed("gen-1", true),
		Config: SharedProjectionRunnerConfig{
			PartitionCount: 1,
			LeaseOwner:     "test-runner",
			PollInterval:   10 * time.Millisecond,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_ = runner.Run(ctx)

	reader.mu.Lock()
	markedCount := len(reader.marked)
	reader.mu.Unlock()

	if markedCount == 0 {
		t.Fatal("expected at least one intent to be marked completed")
	}
}

func TestSharedProjectionRunnerIteratesAllDomains(t *testing.T) {
	t.Parallel()

	reader := &fakeSharedIntentReader{}
	leaseManager := &fakeLeaseManager{granted: false}
	edgeWriter := &fakeEdgeWriter{}

	runner := SharedProjectionRunner{
		IntentReader: reader,
		LeaseManager: leaseManager,
		EdgeWriter:   edgeWriter,
		AcceptedGen:  acceptedGenerationFixed("", false),
		Config: SharedProjectionRunnerConfig{
			PartitionCount: 2,
			PollInterval:   10 * time.Millisecond,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_ = runner.Run(ctx)

	leaseManager.mu.Lock()
	claims := leaseManager.claims
	leaseManager.mu.Unlock()

	wantPerCycle := len(sharedProjectionDomains) * 2
	if claims < wantPerCycle {
		t.Fatalf("expected at least %d lease claims (%d domains * 2 partitions), got %d", wantPerCycle, len(sharedProjectionDomains), claims)
	}
}

func TestSharedProjectionRunnerValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		runner SharedProjectionRunner
	}{
		{
			name:   "nil intent reader",
			runner: SharedProjectionRunner{LeaseManager: &fakeLeaseManager{}, EdgeWriter: &fakeEdgeWriter{}},
		},
		{
			name:   "nil lease manager",
			runner: SharedProjectionRunner{IntentReader: &fakeSharedIntentReader{}, EdgeWriter: &fakeEdgeWriter{}},
		},
		{
			name:   "nil edge writer",
			runner: SharedProjectionRunner{IntentReader: &fakeSharedIntentReader{}, LeaseManager: &fakeLeaseManager{}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.runner.Run(context.Background())
			if err == nil {
				t.Fatal("Run() error = nil, want validation error")
			}
		})
	}
}

func TestSharedProjectionDomainsIncludesAllExpected(t *testing.T) {
	t.Parallel()

	expected := map[string]bool{
		DomainPlatformInfra:      false,
		DomainWorkloadDependency: false,
		DomainInheritanceEdges:   false,
		DomainDocumentationEdges: false,
		DomainRationaleEdges:     false,
		DomainSQLRelationships:   false,
		DomainShellExec:          false,
		DomainHandlesRoute:       false,
		DomainRunsIn:             false,
		DomainInvokesCloudAction: false,
	}

	for _, domain := range sharedProjectionDomains {
		if _, ok := expected[domain]; !ok {
			t.Errorf("unexpected domain in sharedProjectionDomains: %q", domain)
		}
		expected[domain] = true
	}

	for domain, found := range expected {
		if !found {
			t.Errorf("expected domain %q not found in sharedProjectionDomains", domain)
		}
	}

	if got, want := len(sharedProjectionDomains), len(expected); got != want {
		t.Errorf("sharedProjectionDomains length = %d, want %d", got, want)
	}
}

func TestSharedProjectionRunnerProcessesNewDomainIntents(t *testing.T) {
	t.Parallel()

	reader := &fakeSharedIntentReader{
		intents: []SharedProjectionIntentRow{
			{
				IntentID:         "intent-inh-1",
				ProjectionDomain: DomainInheritanceEdges,
				PartitionKey:     "child->parent",
				ScopeID:          "scope-a",
				AcceptanceUnitID: "repo-a",
				RepositoryID:     "repo-a",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				Payload: map[string]any{
					"action":            "upsert",
					"child_entity_id":   "entity:class:child",
					"parent_entity_id":  "entity:class:parent",
					"repo_id":           "repo-a",
					"relationship_type": "INHERITS",
				},
				CreatedAt: time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC),
			},
			{
				IntentID:         "intent-sql-1",
				ProjectionDomain: DomainSQLRelationships,
				PartitionKey:     "view->table",
				ScopeID:          "scope-a",
				AcceptanceUnitID: "repo-a",
				RepositoryID:     "repo-a",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				Payload: map[string]any{
					"action":            "upsert",
					"source_entity_id":  "entity:sql_view:v1",
					"target_entity_id":  "entity:sql_table:t1",
					"repo_id":           "repo-a",
					"relationship_type": "REFERENCES_TABLE",
				},
				CreatedAt: time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC),
			},
		},
	}
	leaseManager := &fakeLeaseManager{granted: true}
	edgeWriter := &fakeEdgeWriter{}

	runner := SharedProjectionRunner{
		IntentReader: reader,
		LeaseManager: leaseManager,
		EdgeWriter:   edgeWriter,
		AcceptedGen:  acceptedGenerationFixed("gen-1", true),
		Config: SharedProjectionRunnerConfig{
			PartitionCount: 1,
			LeaseOwner:     "test-runner",
			PollInterval:   10 * time.Millisecond,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_ = runner.Run(ctx)

	reader.mu.Lock()
	markedCount := len(reader.marked)
	reader.mu.Unlock()

	if markedCount < 2 {
		t.Fatalf("expected at least 2 intents marked completed, got %d", markedCount)
	}
}

func TestSharedProjectionRunnerWithTelemetry(t *testing.T) {
	t.Parallel()

	reader := &fakeSharedIntentReader{
		intents: []SharedProjectionIntentRow{
			{
				IntentID:         "intent-1",
				ProjectionDomain: DomainPlatformInfra,
				PartitionKey:     "platform:eks-prod",
				ScopeID:          "scope-a",
				AcceptanceUnitID: "repo-a",
				RepositoryID:     "repo-a",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				Payload:          map[string]any{"action": "upsert", "repo_id": "repo-a", "platform_id": "p1"},
				CreatedAt:        time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC),
			},
		},
	}
	leaseManager := &fakeLeaseManager{granted: true}
	edgeWriter := &fakeEdgeWriter{}

	tracer := noop.NewTracerProvider().Tracer("test")
	meter := metricnoop.NewMeterProvider().Meter("test")
	instruments, err := telemetry.NewInstruments(meter)
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	logger := slog.Default()

	runner := SharedProjectionRunner{
		IntentReader: reader,
		LeaseManager: leaseManager,
		EdgeWriter:   edgeWriter,
		AcceptedGen:  acceptedGenerationFixed("gen-1", true),
		Config: SharedProjectionRunnerConfig{
			PartitionCount: 1,
			LeaseOwner:     "test-runner",
			PollInterval:   10 * time.Millisecond,
		},
		Tracer:      tracer,
		Instruments: instruments,
		Logger:      logger,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_ = runner.Run(ctx)

	reader.mu.Lock()
	markedCount := len(reader.marked)
	reader.mu.Unlock()

	if markedCount == 0 {
		t.Fatal("expected at least one intent to be marked completed")
	}
}

func TestSharedProjectionRunnerRecordCycleLogsSubstepDurations(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	bootstrap, err := telemetry.NewBootstrap("test-reducer")
	if err != nil {
		t.Fatalf("NewBootstrap() error = %v", err)
	}
	logger := telemetry.NewLoggerWithWriter(bootstrap, "reducer", "reducer", &buf)
	runner := SharedProjectionRunner{Logger: logger}

	runner.recordSharedProjectionCycle(
		context.Background(),
		DomainSQLRelationships,
		0.40,
		PartitionProcessResult{
			MaxIntentWaitSeconds:         8.0,
			ProcessingDurationSeconds:    0.30,
			RetractDurationSeconds:       0.11,
			WriteDurationSeconds:         0.16,
			MarkCompletedDurationSeconds: 0.03,
			SelectionDurationSeconds:     0.07,
			LeaseClaimDurationSeconds:    0.02,
		},
	)

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got, want := entry["processing_duration_seconds"], 0.30; got != want {
		t.Fatalf("processing_duration_seconds = %v, want %v", got, want)
	}
	if got, want := entry["retract_duration_seconds"], 0.11; got != want {
		t.Fatalf("retract_duration_seconds = %v, want %v", got, want)
	}
	if got, want := entry["write_duration_seconds"], 0.16; got != want {
		t.Fatalf("write_duration_seconds = %v, want %v", got, want)
	}
	if got, want := entry["mark_completed_duration_seconds"], 0.03; got != want {
		t.Fatalf("mark_completed_duration_seconds = %v, want %v", got, want)
	}
}

// TestRecordSharedProjectionPartitionMetrics_HistogramAndCounter proves that
// recordSharedProjectionPartitionMetrics emits:
//  1. eshu_dp_shared_projection_partition_processing_seconds with
//     projection_domain and partition_id labels.
//  2. eshu_dp_shared_projection_intents_completed_total with projection_domain
//     label and correct count.
//
// This is the primary drain-path telemetry for #3624 Phase 1.
func TestRecordSharedProjectionPartitionMetrics_HistogramAndCounter(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	runner := SharedProjectionRunner{Instruments: inst}

	const domain = DomainInheritanceEdges
	const partitionID = 3
	const durationSeconds = 0.42

	runner.recordSharedProjectionPartitionMetrics(
		context.Background(),
		domain,
		partitionID,
		durationSeconds,
		PartitionProcessResult{ProcessedIntents: 7},
	)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	// Assert histogram recorded with correct labels.
	histName := "eshu_dp_shared_projection_partition_processing_seconds"
	if !histogramHasAttrsAndValue(rm, histName, map[string]any{
		telemetry.MetricDimensionDomain:      domain,
		telemetry.MetricDimensionPartitionID: int64(partitionID),
	}, durationSeconds) {
		t.Errorf("%s: no data point with domain=%q partition_id=%d duration~=%.3f", histName, domain, partitionID, durationSeconds)
	}

	// Assert counter recorded with correct domain and count.
	counterName := "eshu_dp_shared_projection_intents_completed_total"
	if !counterHasValue(rm, counterName, domain, 7) {
		t.Errorf("%s: expected count=7 for domain=%q", counterName, domain)
	}
}

// TestRecordSharedProjectionPartitionMetrics_SkipsZeroDuration proves that a
// zero total-duration cycle (e.g. lease not acquired, no timing recorded) does
// not emit a spurious zero histogram bucket.
func TestRecordSharedProjectionPartitionMetrics_SkipsZeroDuration(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	runner := SharedProjectionRunner{Instruments: inst}

	runner.recordSharedProjectionPartitionMetrics(
		context.Background(),
		DomainSQLRelationships,
		0,
		0.0, // zero duration — must be skipped
		PartitionProcessResult{ProcessedIntents: 0},
	)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	// Neither instrument should have any data points.
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch m.Name {
			case "eshu_dp_shared_projection_partition_processing_seconds",
				"eshu_dp_shared_projection_intents_completed_total":
				t.Errorf("unexpected metric %q emitted for zero-duration zero-processed cycle", m.Name)
			}
		}
	}
}

// TestRecordSharedProjectionPartitionMetrics_CardinalityBounded proves that
// the instruments only carry bounded dimension keys (domain + partition_id).
// Raw intent IDs, scope IDs, and generation IDs must never appear.
func TestRecordSharedProjectionPartitionMetrics_CardinalityBounded(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	runner := SharedProjectionRunner{Instruments: inst}
	runner.recordSharedProjectionPartitionMetrics(
		context.Background(),
		DomainHandlesRoute,
		5,
		0.10,
		PartitionProcessResult{ProcessedIntents: 3},
	)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	forbidden := []string{"intent_id", "scope_id", "generation_id", "acceptance_unit_id", "repository_id"}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch m.Name {
			case "eshu_dp_shared_projection_partition_processing_seconds":
				hist, ok := m.Data.(metricdata.Histogram[float64])
				if !ok {
					continue
				}
				for _, dp := range hist.DataPoints {
					for _, key := range forbidden {
						if _, ok := dp.Attributes.Value(attribute.Key(key)); ok {
							t.Errorf("%s data point carries forbidden label %q", m.Name, key)
						}
					}
				}
			case "eshu_dp_shared_projection_intents_completed_total":
				sum, ok := m.Data.(metricdata.Sum[int64])
				if !ok {
					continue
				}
				for _, dp := range sum.DataPoints {
					for _, key := range forbidden {
						if _, ok := dp.Attributes.Value(attribute.Key(key)); ok {
							t.Errorf("%s data point carries forbidden label %q", m.Name, key)
						}
					}
				}
			}
		}
	}
}

// histogramHasAttrsAndValue checks that at least one histogram data point
// matches the given attribute map and has a sum within 1% of wantSum.
// Attribute values may be string or int64.
func histogramHasAttrsAndValue(rm metricdata.ResourceMetrics, name string, attrs map[string]any, wantSum float64) bool {
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				continue
			}
			for _, dp := range hist.DataPoints {
				match := true
				for key, want := range attrs {
					got, ok := dp.Attributes.Value(attribute.Key(key))
					if !ok {
						match = false
						break
					}
					var gotVal any
					switch got.Type() {
					case attribute.STRING:
						gotVal = got.AsString()
					case attribute.INT64:
						gotVal = got.AsInt64()
					default:
						match = false
					}
					if gotVal != want {
						match = false
						break
					}
				}
				if match && dp.Count > 0 {
					// wantSum should appear in the histogram sum (within float tolerance).
					diff := dp.Sum - wantSum
					if diff < 0 {
						diff = -diff
					}
					if diff <= wantSum*0.01+0.0001 {
						return true
					}
				}
			}
		}
	}
	return false
}

// counterHasValue checks that a Sum[int64] metric carries at least one data
// point with the given domain label and value.
func counterHasValue(rm metricdata.ResourceMetrics, name, domain string, wantValue int64) bool {
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				got, ok := dp.Attributes.Value(attribute.Key(telemetry.MetricDimensionDomain))
				if !ok {
					continue
				}
				if got.AsString() == domain && dp.Value == wantValue {
					return true
				}
			}
		}
	}
	return false
}
