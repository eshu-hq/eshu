package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestCodeCallMaterializationHandlerRejectsWrongDomain(t *testing.T) {
	t.Parallel()

	handler := CodeCallMaterializationHandler{
		IntentWriter: &recordingCodeCallIntentWriter{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainWorkloadIdentity,
		EnqueuedAt:   time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC),
		AvailableAt:  time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC),
		Status:       IntentStatusPending,
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want non-nil")
	}
}

func TestCodeCallMaterializationHandlerRequiresIntentWriter(t *testing.T) {
	t.Parallel()

	handler := CodeCallMaterializationHandler{
		FactLoader: &stubFactLoader{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainCodeCallMaterialization,
		EnqueuedAt:   time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC),
		AvailableAt:  time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC),
		Status:       IntentStatusPending,
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want non-nil")
	}
}

func TestCodeCallMaterializationHandlerEmitsSharedIntents(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	loader := &stubFactLoader{
		envelopes: []facts.Envelope{
			{
				FactKind: "repository",
				Payload: map[string]any{
					"repo_id":       "repo-a",
					"source_run_id": "run-a",
					"graph_id":      "repo-a",
					"graph_kind":    "repository",
					"name":          "repo-a",
				},
			},
			{
				FactKind: "repository",
				Payload: map[string]any{
					"repo_id":       "repo-b",
					"source_run_id": "run-b",
					"graph_id":      "repo-b",
					"graph_kind":    "repository",
					"name":          "repo-b",
				},
			},
			{
				FactKind: "file",
				Payload: map[string]any{
					"repo_id":       "repo-a",
					"relative_path": "caller.py",
					"parsed_file_data": map[string]any{
						"path": "caller.py",
						"functions": []any{
							map[string]any{
								"name":        "handle",
								"line_number": 3,
								"uid":         "entity:handle",
							},
						},
						"function_calls_scip": []any{
							map[string]any{
								"caller_file":   "caller.py",
								"caller_line":   3,
								"caller_symbol": "pkg/caller#handle().",
								"callee_file":   "callee.py",
								"callee_line":   1,
								"callee_symbol": "pkg/callee#callee().",
							},
						},
					},
				},
			},
			{
				FactKind: "file",
				Payload: map[string]any{
					"repo_id":       "repo-a",
					"relative_path": "callee.py",
					"parsed_file_data": map[string]any{
						"path": "callee.py",
						"functions": []any{
							map[string]any{
								"name":        "callee",
								"line_number": 1,
								"uid":         "entity:callee",
							},
						},
					},
				},
			},
			{
				FactKind: "file",
				Payload: map[string]any{
					"repo_id":       "repo-a",
					"relative_path": "models.py",
					"parsed_file_data": map[string]any{
						"path": "models.py",
						"classes": []any{
							map[string]any{
								"name":      "Widget",
								"uid":       "entity:widget",
								"metaclass": "Meta",
							},
							map[string]any{
								"name": "Meta",
								"uid":  "entity:meta",
							},
						},
					},
				},
			},
		},
	}

	writer := &recordingCodeCallIntentWriter{}
	handler := CodeCallMaterializationHandler{
		FactLoader:   loader,
		IntentWriter: writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-code-call-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		SourceSystem: "git",
		Domain:       DomainCodeCallMaterialization,
		Cause:        "parser follow-up required",
		EnqueuedAt:   now,
		AvailableAt:  now,
		Status:       IntentStatusPending,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("result.Status = %q, want %q", result.Status, ResultStatusSucceeded)
	}
	if result.CanonicalWrites != 4 {
		t.Fatalf("result.CanonicalWrites = %d, want 4", result.CanonicalWrites)
	}
	if len(writer.rows) != 4 {
		t.Fatalf("len(writer.rows) = %d, want 4", len(writer.rows))
	}

	rowsByRepo := make(map[string][]SharedProjectionIntentRow)
	for _, row := range writer.rows {
		rowsByRepo[row.RepositoryID] = append(rowsByRepo[row.RepositoryID], row)
		if row.GenerationID != "gen-1" {
			t.Fatalf("row.GenerationID = %q, want gen-1", row.GenerationID)
		}
	}

	if got := len(rowsByRepo["repo-a"]); got != 3 {
		t.Fatalf("repo-a rows = %d, want 3", got)
	}
	if got := len(rowsByRepo["repo-b"]); got != 1 {
		t.Fatalf("repo-b rows = %d, want 1", got)
	}

	var (
		refreshRows   []SharedProjectionIntentRow
		codeCallRows  []SharedProjectionIntentRow
		metaclassRows []SharedProjectionIntentRow
	)
	for _, row := range writer.rows {
		switch row.Payload["intent_type"] {
		case "repo_refresh":
			refreshRows = append(refreshRows, row)
		default:
			switch row.Payload["evidence_source"] {
			case codeCallEvidenceSource:
				codeCallRows = append(codeCallRows, row)
			case pythonMetaclassEvidenceSource:
				metaclassRows = append(metaclassRows, row)
			}
		}
	}

	if len(refreshRows) != 2 {
		t.Fatalf("len(refreshRows) = %d, want 2", len(refreshRows))
	}
	if len(codeCallRows) != 1 {
		t.Fatalf("len(codeCallRows) = %d, want 1", len(codeCallRows))
	}
	if len(metaclassRows) != 1 {
		t.Fatalf("len(metaclassRows) = %d, want 1", len(metaclassRows))
	}

	refreshRuns := make(map[string]string, len(refreshRows))
	for _, row := range refreshRows {
		refreshRuns[row.RepositoryID] = row.SourceRunID
		if got, want := row.PartitionKey, codeCallRefreshPartitionKey(row.RepositoryID); got != want {
			t.Fatalf("refresh PartitionKey = %q, want %q", got, want)
		}
	}
	if got, want := refreshRuns["repo-a"], "run-a"; got != want {
		t.Fatalf("refresh SourceRunID for repo-a = %q, want %q", got, want)
	}
	if got, want := refreshRuns["repo-b"], "run-b"; got != want {
		t.Fatalf("refresh SourceRunID for repo-b = %q, want %q", got, want)
	}

	for _, row := range refreshRows {
		if row.Payload["action"] != "refresh" {
			t.Fatalf("refresh action = %#v, want refresh", row.Payload["action"])
		}
		if row.Payload["repo_id"] == "" {
			t.Fatal("refresh row missing repo_id")
		}
		if _, ok := row.Payload["caller_entity_id"]; ok {
			t.Fatal("refresh row unexpectedly contains caller_entity_id")
		}
		if _, ok := row.Payload["source_entity_id"]; ok {
			t.Fatal("refresh row unexpectedly contains source_entity_id")
		}
	}

	if got, want := codeCallRows[0].SourceRunID, "run-a"; got != want {
		t.Fatalf("code-call SourceRunID = %q, want %q", got, want)
	}
	if got, want := codeCallRows[0].Payload["evidence_source"], codeCallEvidenceSource; got != want {
		t.Fatalf("code-call evidence_source = %#v, want %#v", got, want)
	}
	if got, want := codeCallRows[0].Payload["caller_entity_id"], "entity:handle"; got != want {
		t.Fatalf("code-call caller_entity_id = %#v, want %#v", got, want)
	}
	if got, want := codeCallRows[0].Payload["callee_entity_id"], "entity:callee"; got != want {
		t.Fatalf("code-call callee_entity_id = %#v, want %#v", got, want)
	}
	if got, want := codeCallRows[0].PartitionKey, "entity:handle->entity:callee"; got != want {
		t.Fatalf("code-call PartitionKey = %q, want %q", got, want)
	}

	if got, want := metaclassRows[0].SourceRunID, "run-a"; got != want {
		t.Fatalf("metaclass SourceRunID = %q, want %q", got, want)
	}
	if got, want := metaclassRows[0].Payload["evidence_source"], pythonMetaclassEvidenceSource; got != want {
		t.Fatalf("metaclass evidence_source = %#v, want %#v", got, want)
	}
	if got, want := metaclassRows[0].Payload["relationship_type"], "USES_METACLASS"; got != want {
		t.Fatalf("metaclass relationship_type = %#v, want %#v", got, want)
	}
	if got, want := metaclassRows[0].PartitionKey, "entity:widget->entity:meta"; got != want {
		t.Fatalf("metaclass PartitionKey = %q, want %q", got, want)
	}
}

func TestBuildCodeCallSharedIntentRowsDeduplicatesIntentIdentity(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.April, 17, 15, 0, 0, 0, time.UTC)
	contextByRepoID := map[string]ProjectionContext{
		"repo-a": {
			ScopeID:          "scope:git:repo-a",
			AcceptanceUnitID: "repository:repo-a",
			SourceRunID:      "run-a",
			GenerationID:     "gen-a",
		},
	}

	rows := []map[string]any{
		{
			"repo_id":          "repo-a",
			"caller_entity_id": "entity:caller",
			"callee_entity_id": "entity:callee",
			"caller_file":      "caller.py",
			"callee_file":      "callee.py",
			"callsite_line":    10,
			"evidence_source":  codeCallEvidenceSource,
			"edge_observation": "callsite-a",
			"source_entity_id": "entity:caller",
			"target_entity_id": "entity:callee",
		},
		{
			"repo_id":          "repo-a",
			"caller_entity_id": "entity:caller",
			"callee_entity_id": "entity:callee",
			"caller_file":      "caller.py",
			"callee_file":      "callee.py",
			"callsite_line":    42,
			"evidence_source":  codeCallEvidenceSource,
			"edge_observation": "callsite-b",
			"source_entity_id": "entity:caller",
			"target_entity_id": "entity:callee",
		},
	}

	intents := buildCodeCallSharedIntentRows(rows, contextByRepoID, createdAt, codeCallEvidenceSource)
	if got, want := len(intents), 1; got != want {
		t.Fatalf("len(intents) = %d, want %d", got, want)
	}
	if got, want := intents[0].IntentID, BuildSharedProjectionIntent(SharedProjectionIntentInput{
		ProjectionDomain: DomainCodeCalls,
		PartitionKey:     "entity:caller->entity:callee",
		ScopeID:          "scope:git:repo-a",
		AcceptanceUnitID: "repository:repo-a",
		RepositoryID:     "repo-a",
		SourceRunID:      "run-a",
		GenerationID:     "gen-a",
		Payload: map[string]any{
			"repo_id":          "repo-a",
			"caller_entity_id": "entity:caller",
			"callee_entity_id": "entity:callee",
			"caller_file":      "caller.py",
			"callee_file":      "callee.py",
			"callsite_line":    10,
			"evidence_source":  codeCallEvidenceSource,
			"edge_observation": "callsite-a",
			"source_entity_id": "entity:caller",
			"target_entity_id": "entity:callee",
		},
		CreatedAt: createdAt,
	}).IntentID; got != want {
		t.Fatalf("IntentID = %q, want %q", got, want)
	}
}

type recordingCodeCallIntentWriter struct {
	rows []SharedProjectionIntentRow
}

func (r *recordingCodeCallIntentWriter) UpsertIntents(_ context.Context, rows []SharedProjectionIntentRow) error {
	r.rows = append(r.rows, rows...)
	return nil
}
