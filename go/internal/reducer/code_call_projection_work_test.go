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

func TestCodeCallProjectionRunnerRetractRepoPreservesDeltaFileScope(t *testing.T) {
	t.Parallel()

	writer := &recordingCodeCallProjectionEdgeWriter{}
	runner := CodeCallProjectionRunner{EdgeWriter: writer}
	rows := []SharedProjectionIntentRow{
		{
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"repo_id":           "repo-a",
				"delta_projection":  true,
				"delta_file_paths":  []string{"/repo/src/changed.go"},
				"caller_entity_id":  "caller",
				"callee_entity_id":  "callee",
				"evidence_source":   codeCallEvidenceSource,
				"relationship_type": "CALLS",
			},
		},
	}

	if err := runner.retractRepo(context.Background(), rows); err != nil {
		t.Fatalf("retractRepo() error = %v, want nil", err)
	}
	if len(writer.retractCalls) != 2 {
		t.Fatalf("len(retractCalls) = %d, want 2 evidence-source retracts", len(writer.retractCalls))
	}
	for i, call := range writer.retractCalls {
		if len(call.rows) != 1 {
			t.Fatalf("retractCalls[%d].rows len = %d, want 1", i, len(call.rows))
		}
		payload := call.rows[0].Payload
		if got, ok := payload["delta_projection"].(bool); !ok || !got {
			t.Fatalf("retractCalls[%d] delta_projection = %#v, want true", i, payload["delta_projection"])
		}
		gotPaths, ok := payload["delta_file_paths"].([]string)
		if !ok {
			t.Fatalf("retractCalls[%d] delta_file_paths type = %T, want []string", i, payload["delta_file_paths"])
		}
		if len(gotPaths) != 1 || gotPaths[0] != "/repo/src/changed.go" {
			t.Fatalf("retractCalls[%d] delta_file_paths = %#v, want [/repo/src/changed.go]", i, gotPaths)
		}
	}
}

func TestCodeCallProjectionRunnerRetractRepoPreservesDeletedOnlyDeltaFileScope(t *testing.T) {
	t.Parallel()

	writer := &recordingCodeCallProjectionEdgeWriter{}
	runner := CodeCallProjectionRunner{EdgeWriter: writer}
	rows := []SharedProjectionIntentRow{
		{
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"repo_id":          "repo-a",
				"delta_projection": true,
				"delta_file_paths": []string{"/repo/src/deleted.go"},
				"intent_type":      "repo_refresh",
			},
		},
	}

	if err := runner.retractRepo(context.Background(), rows); err != nil {
		t.Fatalf("retractRepo() error = %v, want nil", err)
	}
	if len(writer.retractCalls) != 2 {
		t.Fatalf("len(retractCalls) = %d, want 2 evidence-source retracts", len(writer.retractCalls))
	}
	for i, call := range writer.retractCalls {
		payload := call.rows[0].Payload
		gotPaths, ok := payload["delta_file_paths"].([]string)
		if !ok {
			t.Fatalf("retractCalls[%d] delta_file_paths type = %T, want []string", i, payload["delta_file_paths"])
		}
		if len(gotPaths) != 1 || gotPaths[0] != "/repo/src/deleted.go" {
			t.Fatalf("retractCalls[%d] delta_file_paths = %#v, want [/repo/src/deleted.go]", i, gotPaths)
		}
	}
}

func TestBuildCodeCallRetractRowsKeepsMalformedDeltaScoped(t *testing.T) {
	t.Parallel()

	rows := buildCodeCallRetractRows([]SharedProjectionIntentRow{
		{
			RepositoryID: "repo-a",
			Payload: map[string]any{
				"repo_id":          "repo-a",
				"delta_projection": true,
			},
		},
	})
	if len(rows) != 1 {
		t.Fatalf("retract rows len = %d, want 1", len(rows))
	}
	payload := rows[0].Payload
	if got, ok := payload["delta_projection"].(bool); !ok || !got {
		t.Fatalf("delta_projection = %#v, want true", payload["delta_projection"])
	}
	if gotPaths := semanticPayloadStringSlice(payload, "delta_file_paths"); len(gotPaths) != 0 {
		t.Fatalf("delta_file_paths = %#v, want empty malformed delta scope", gotPaths)
	}
}
