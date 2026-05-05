package reducer

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestSharedProjectionRunnerBackoffOnEmptyCycles(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var waits []time.Duration

	runner := SharedProjectionRunner{
		IntentReader: &fakeSharedIntentReader{},
		LeaseManager: &fakeLeaseManager{granted: false},
		EdgeWriter:   &fakeEdgeWriter{},
		AcceptedGen:  acceptedGenerationFixed("", false),
		Config: SharedProjectionRunnerConfig{
			PartitionCount: 1,
			PollInterval:   500 * time.Millisecond,
		},
		Wait: func(_ context.Context, d time.Duration) error {
			mu.Lock()
			waits = append(waits, d)
			mu.Unlock()
			if len(waits) >= 5 {
				return context.Canceled
			}
			return nil
		},
	}

	_ = runner.Run(context.Background())

	mu.Lock()
	defer mu.Unlock()

	expected := []time.Duration{
		500 * time.Millisecond,
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		4 * time.Second,
	}
	if len(waits) < len(expected) {
		t.Fatalf("wait calls = %d, want at least %d", len(waits), len(expected))
	}
	for i, want := range expected {
		if waits[i] != want {
			t.Errorf("wait[%d] = %v, want %v", i, waits[i], want)
		}
	}
}

func TestSharedProjectionRunnerUsesBasePollIntervalWhileReadinessBlocked(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var waits []time.Duration
	reader := &fakeSharedIntentReader{
		intents: []SharedProjectionIntentRow{
			{
				IntentID:         "blocked-sql-1",
				ProjectionDomain: DomainSQLRelationships,
				PartitionKey:     "view->table",
				ScopeID:          "scope-a",
				AcceptanceUnitID: "repo-a",
				RepositoryID:     "repo-a",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				Payload: map[string]any{
					"action":           "upsert",
					"source_entity_id": "entity:sql_view:v1",
					"target_entity_id": "entity:sql_table:t1",
				},
				CreatedAt: time.Date(2026, 4, 17, 13, 0, 0, 0, time.UTC),
			},
		},
	}

	runner := SharedProjectionRunner{
		IntentReader:    reader,
		LeaseManager:    &fakeLeaseManager{granted: true},
		EdgeWriter:      &fakeEdgeWriter{},
		AcceptedGen:     acceptedGenerationFixed("gen-1", true),
		ReadinessLookup: readinessLookupFixed(false, false),
		Config: SharedProjectionRunnerConfig{
			PartitionCount: 1,
			PollInterval:   500 * time.Millisecond,
		},
		Wait: func(_ context.Context, d time.Duration) error {
			mu.Lock()
			waits = append(waits, d)
			mu.Unlock()
			if len(waits) >= 3 {
				return context.Canceled
			}
			return nil
		},
	}

	_ = runner.Run(context.Background())

	mu.Lock()
	defer mu.Unlock()

	expected := []time.Duration{
		500 * time.Millisecond,
		500 * time.Millisecond,
		500 * time.Millisecond,
	}
	if len(waits) < len(expected) {
		t.Fatalf("wait calls = %d, want at least %d", len(waits), len(expected))
	}
	for i, want := range expected {
		if waits[i] != want {
			t.Errorf("wait[%d] = %v, want %v", i, waits[i], want)
		}
	}

	reader.mu.Lock()
	markedCount := len(reader.marked)
	reader.mu.Unlock()
	if markedCount != 0 {
		t.Fatalf("marked intents = %d, want 0 while readiness is blocked", markedCount)
	}
}

func TestSharedProjectionRunnerBackoffResetsOnWork(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var waits []time.Duration
	waitCount := 0

	reader := &fakeSharedIntentReader{}
	leaseManager := &fakeLeaseManager{granted: true}

	runner := SharedProjectionRunner{
		IntentReader: reader,
		LeaseManager: leaseManager,
		EdgeWriter:   &fakeEdgeWriter{},
		AcceptedGen:  acceptedGenerationFixed("gen-1", true),
		Config: SharedProjectionRunnerConfig{
			PartitionCount: 1,
			PollInterval:   500 * time.Millisecond,
		},
		Wait: func(_ context.Context, d time.Duration) error {
			mu.Lock()
			waits = append(waits, d)
			waitCount++
			wc := waitCount
			mu.Unlock()

			if wc == 2 {
				reader.mu.Lock()
				reader.intents = append(reader.intents, SharedProjectionIntentRow{
					IntentID:         "intent-reset",
					ProjectionDomain: DomainPlatformInfra,
					PartitionKey:     "platform:test",
					ScopeID:          "scope-b",
					AcceptanceUnitID: "repo-b",
					RepositoryID:     "repo-b",
					SourceRunID:      "run-1",
					GenerationID:     "gen-1",
					Payload:          map[string]any{"action": "upsert", "repo_id": "repo-b", "platform_id": "p2"},
					CreatedAt:        time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC),
				})
				reader.mu.Unlock()
			}
			if wc >= 4 {
				return context.Canceled
			}
			return nil
		},
	}

	_ = runner.Run(context.Background())

	mu.Lock()
	defer mu.Unlock()

	if len(waits) < 3 {
		t.Fatalf("wait calls = %d, want at least 3", len(waits))
	}
	if waits[0] != 500*time.Millisecond {
		t.Errorf("wait[0] = %v, want 500ms", waits[0])
	}
	if waits[1] != 1*time.Second {
		t.Errorf("wait[1] = %v, want 1s", waits[1])
	}
	for i := 2; i < len(waits); i++ {
		if waits[i] == 500*time.Millisecond {
			return
		}
	}
	t.Errorf("backoff never reset to base interval after work; waits = %v", waits)
}
