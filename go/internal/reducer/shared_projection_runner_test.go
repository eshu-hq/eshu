package reducer

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	metricnoop "go.opentelemetry.io/otel/metric/noop"
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
		DomainSQLRelationships:   false,
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
