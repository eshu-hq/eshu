package reducer

import (
	"context"
	"testing"
	"time"
)

func TestCodeCallProjectionRunnerUsesBasePollIntervalWhileReadinessBlocked(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 28, 17, 0, 0, 0, time.UTC)
	reader := &fakeCodeCallIntentStore{
		pendingByDomain: []SharedProjectionIntentRow{
			{
				IntentID:         "blocked-1",
				ProjectionDomain: DomainCodeCalls,
				PartitionKey:     "caller->callee",
				ScopeID:          "scope-a",
				AcceptanceUnitID: "repo-a",
				RepositoryID:     "repo-a",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				CreatedAt:        now.Add(-time.Minute),
			},
		},
		leaseGranted: true,
	}

	var waits []time.Duration
	runner := CodeCallProjectionRunner{
		IntentReader:    reader,
		LeaseManager:    reader,
		EdgeWriter:      &recordingCodeCallProjectionEdgeWriter{},
		AcceptedGen:     acceptedGenerationFixed("gen-1", true),
		ReadinessLookup: readinessLookupFixed(false, false),
		Config: CodeCallProjectionRunnerConfig{
			PollInterval: 500 * time.Millisecond,
			BatchLimit:   10,
		},
		Wait: func(_ context.Context, interval time.Duration) error {
			waits = append(waits, interval)
			if len(waits) >= 3 {
				return context.Canceled
			}
			return nil
		},
	}

	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	want := []time.Duration{
		500 * time.Millisecond,
		500 * time.Millisecond,
		500 * time.Millisecond,
	}
	if len(waits) != len(want) {
		t.Fatalf("waits = %v, want %v", waits, want)
	}
	for i := range want {
		if waits[i] != want[i] {
			t.Fatalf("waits[%d] = %v, want %v", i, waits[i], want[i])
		}
	}
	if len(reader.marked) != 0 {
		t.Fatalf("marked = %v, want no completed intents while readiness is blocked", reader.marked)
	}
}
