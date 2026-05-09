package reducer

import (
	"context"
	"strconv"
	"testing"
	"time"
)

func TestCodeCallProjectionRunnerLoadAllAcceptanceUnitIntentsReturnsCappedChunk(t *testing.T) {
	t.Parallel()

	reader := &fakeCodeCallIntentStore{
		acceptanceResponder: func(_ SharedProjectionAcceptanceKey, limit int) ([]SharedProjectionIntentRow, error) {
			rows := make([]SharedProjectionIntentRow, limit)
			for i := range rows {
				rows[i] = SharedProjectionIntentRow{
					IntentID:         "intent",
					ProjectionDomain: DomainCodeCalls,
					ScopeID:          "scope-a",
					AcceptanceUnitID: "repo-a",
					RepositoryID:     "repo-a",
					SourceRunID:      "run-1",
					GenerationID:     "gen-1",
				}
			}
			return rows, nil
		},
	}
	runner := CodeCallProjectionRunner{
		IntentReader: reader,
		Config: CodeCallProjectionRunnerConfig{
			BatchLimit:          100,
			AcceptanceScanLimit: 1_000,
		},
	}

	got, err := runner.loadAllAcceptanceUnitIntents(context.Background(), SharedProjectionAcceptanceKey{
		ScopeID:          "scope-a",
		AcceptanceUnitID: "repo-a",
		SourceRunID:      "run-1",
	})
	if err != nil {
		t.Fatalf("loadAllAcceptanceUnitIntents() error = %v, want nil", err)
	}
	if len(got) != 1_000 {
		t.Fatalf("loaded rows = %d, want capped chunk %d", len(got), 1_000)
	}
	if got, want := reader.acceptanceLimitRequests[len(reader.acceptanceLimitRequests)-1], 1_000; got != want {
		t.Fatalf("final acceptance scan limit = %d, want cap %d", got, want)
	}
	if len(reader.acceptanceLimitRequests) < 2 {
		t.Fatalf("acceptanceLimitRequests = %v, want growth up to cap", reader.acceptanceLimitRequests)
	}
}

func TestCodeCallProjectionRunnerLoadAllAcceptanceUnitIntentsAllowsLargeConfiguredSlice(t *testing.T) {
	t.Parallel()

	const rowCount = 10_001
	rows := make([]SharedProjectionIntentRow, rowCount)
	for i := range rows {
		rows[i] = SharedProjectionIntentRow{
			IntentID:         "intent-" + strconv.Itoa(i),
			ProjectionDomain: DomainCodeCalls,
			ScopeID:          "scope-a",
			AcceptanceUnitID: "repo-a",
			RepositoryID:     "repo-a",
			SourceRunID:      "run-1",
			GenerationID:     "gen-1",
			CreatedAt:        time.Date(2026, time.April, 27, 9, 0, 0, i, time.UTC),
		}
	}
	reader := &fakeCodeCallIntentStore{
		pendingByAcceptance: map[string][]SharedProjectionIntentRow{
			"scope-a|repo-a|run-1": rows,
		},
	}
	runner := CodeCallProjectionRunner{
		IntentReader: reader,
		Config: CodeCallProjectionRunnerConfig{
			BatchLimit:          100,
			AcceptanceScanLimit: 20_000,
		},
	}

	got, err := runner.loadAllAcceptanceUnitIntents(context.Background(), SharedProjectionAcceptanceKey{
		ScopeID:          "scope-a",
		AcceptanceUnitID: "repo-a",
		SourceRunID:      "run-1",
	})
	if err != nil {
		t.Fatalf("loadAllAcceptanceUnitIntents() error = %v, want nil", err)
	}
	if len(got) != rowCount {
		t.Fatalf("loaded rows = %d, want %d", len(got), rowCount)
	}
	if gotLimit := reader.acceptanceLimitRequests[len(reader.acceptanceLimitRequests)-1]; gotLimit <= rowCount {
		t.Fatalf("final acceptance scan limit = %d, want larger than row count %d", gotLimit, rowCount)
	}
}
