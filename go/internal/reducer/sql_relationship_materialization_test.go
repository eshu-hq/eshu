package reducer

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestSQLRelationshipHandlerRejectsMismatchedDomain(t *testing.T) {
	t.Parallel()

	handler := SQLRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{},
		EdgeWriter: &recordingSQLRelEdgeWriter{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainCodeCallMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err == nil {
		t.Fatal("expected error for mismatched domain")
	}
}

func TestSQLRelationshipHandlerRequiresFactLoader(t *testing.T) {
	t.Parallel()

	handler := SQLRelationshipMaterializationHandler{
		EdgeWriter: &recordingSQLRelEdgeWriter{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainSQLRelationshipMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err == nil {
		t.Fatal("expected error when fact loader is nil")
	}
}

func TestSQLRelationshipHandlerRequiresEdgeWriter(t *testing.T) {
	t.Parallel()

	handler := SQLRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       DomainSQLRelationshipMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err == nil {
		t.Fatal("expected error when edge writer is nil")
	}
}

func TestExtractSQLRelationshipRowsEmptyInputReturnsNil(t *testing.T) {
	t.Parallel()

	repoIDs, rows := ExtractSQLRelationshipRows(nil)
	if repoIDs != nil {
		t.Fatalf("repoIDs = %v, want nil", repoIDs)
	}
	if rows != nil {
		t.Fatalf("rows = %v, want nil", rows)
	}
}

func TestExtractSQLRelationshipRowsNoRelationshipsReturnsEmpty(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-123",
				"entity_id":   "content-entity:e_tbl1",
				"entity_type": "SqlTable",
				"entity_name": "public.users",
			},
		},
	}

	repoIDs, rows := ExtractSQLRelationshipRows(envelopes)
	if len(repoIDs) != 1 || repoIDs[0] != "repo-123" {
		t.Fatalf("repoIDs = %v, want [repo-123]", repoIDs)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0", len(rows))
	}
}

func TestExtractSQLRelationshipRowsFromViewReferencingTable(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-123",
				"entity_id":   "content-entity:e_tbl1",
				"entity_type": "SqlTable",
				"entity_name": "public.users",
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-123",
				"entity_id":   "content-entity:e_view1",
				"entity_type": "SqlView",
				"entity_name": "public.active_users",
				"entity_metadata": map[string]any{
					"source_tables":   []any{"public.users"},
					"sql_entity_type": "SqlView",
				},
			},
		},
	}

	repoIDs, rows := ExtractSQLRelationshipRows(envelopes)
	if len(repoIDs) != 1 || repoIDs[0] != "repo-123" {
		t.Fatalf("repoIDs = %v, want [repo-123]", repoIDs)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got, want := rows[0]["source_entity_id"], "content-entity:e_view1"; got != want {
		t.Fatalf("source_entity_id = %v, want %v", got, want)
	}
	if got, want := rows[0]["target_entity_id"], "content-entity:e_tbl1"; got != want {
		t.Fatalf("target_entity_id = %v, want %v", got, want)
	}
	if got, want := rows[0]["relationship_type"], "REFERENCES_TABLE"; got != want {
		t.Fatalf("relationship_type = %v, want %v", got, want)
	}
	if got, want := rows[0]["source_entity_type"], "SqlView"; got != want {
		t.Fatalf("source_entity_type = %v, want %v", got, want)
	}
	if got, want := rows[0]["target_entity_type"], "SqlTable"; got != want {
		t.Fatalf("target_entity_type = %v, want %v", got, want)
	}
	if got, want := rows[0]["repo_id"], "repo-123"; got != want {
		t.Fatalf("repo_id = %v, want %v", got, want)
	}
}

func TestExtractSQLRelationshipRowsFromTableWithColumn(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-123",
				"entity_id":   "content-entity:e_tbl1",
				"entity_type": "SqlTable",
				"entity_name": "public.users",
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-123",
				"entity_id":   "content-entity:e_col1",
				"entity_type": "SqlColumn",
				"entity_name": "public.users.email",
				"entity_metadata": map[string]any{
					"table_name":      "public.users",
					"sql_entity_type": "SqlColumn",
				},
			},
		},
	}

	repoIDs, rows := ExtractSQLRelationshipRows(envelopes)
	if len(repoIDs) != 1 || repoIDs[0] != "repo-123" {
		t.Fatalf("repoIDs = %v, want [repo-123]", repoIDs)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got, want := rows[0]["source_entity_id"], "content-entity:e_tbl1"; got != want {
		t.Fatalf("source_entity_id = %v, want %v", got, want)
	}
	if got, want := rows[0]["target_entity_id"], "content-entity:e_col1"; got != want {
		t.Fatalf("target_entity_id = %v, want %v", got, want)
	}
	if got, want := rows[0]["relationship_type"], "HAS_COLUMN"; got != want {
		t.Fatalf("relationship_type = %v, want %v", got, want)
	}
	if got, want := rows[0]["source_entity_type"], "SqlTable"; got != want {
		t.Fatalf("source_entity_type = %v, want %v", got, want)
	}
	if got, want := rows[0]["target_entity_type"], "SqlColumn"; got != want {
		t.Fatalf("target_entity_type = %v, want %v", got, want)
	}
}

func TestExtractSQLRelationshipRowsFromTriggerOnTable(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-123",
				"entity_id":   "content-entity:e_tbl1",
				"entity_type": "SqlTable",
				"entity_name": "public.users",
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-123",
				"entity_id":   "content-entity:e_trig1",
				"entity_type": "SqlTrigger",
				"entity_name": "users_touch",
				"entity_metadata": map[string]any{
					"table_name":      "public.users",
					"sql_entity_type": "SqlTrigger",
				},
			},
		},
	}

	repoIDs, rows := ExtractSQLRelationshipRows(envelopes)
	if len(repoIDs) != 1 || repoIDs[0] != "repo-123" {
		t.Fatalf("repoIDs = %v, want [repo-123]", repoIDs)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got, want := rows[0]["source_entity_id"], "content-entity:e_trig1"; got != want {
		t.Fatalf("source_entity_id = %v, want %v", got, want)
	}
	if got, want := rows[0]["target_entity_id"], "content-entity:e_tbl1"; got != want {
		t.Fatalf("target_entity_id = %v, want %v", got, want)
	}
	if got, want := rows[0]["relationship_type"], "TRIGGERS"; got != want {
		t.Fatalf("relationship_type = %v, want %v", got, want)
	}
	if got, want := rows[0]["source_entity_type"], "SqlTrigger"; got != want {
		t.Fatalf("source_entity_type = %v, want %v", got, want)
	}
	if got, want := rows[0]["target_entity_type"], "SqlTable"; got != want {
		t.Fatalf("target_entity_type = %v, want %v", got, want)
	}
}

func TestExtractSQLRelationshipRowsDeduplicates(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-123",
				"entity_id":   "content-entity:e_tbl1",
				"entity_type": "SqlTable",
				"entity_name": "public.users",
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-123",
				"entity_id":   "content-entity:e_view1",
				"entity_type": "SqlView",
				"entity_name": "public.active_users",
				"entity_metadata": map[string]any{
					"source_tables":   []any{"public.users", "public.users"},
					"sql_entity_type": "SqlView",
				},
			},
		},
	}

	_, rows := ExtractSQLRelationshipRows(envelopes)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (deduplication)", len(rows))
	}
}

func TestSQLRelationshipHandlerWritesEdges(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	writer := &recordingSQLRelEdgeWriter{}
	handler := SQLRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{
			envelopes: []facts.Envelope{
				{
					FactKind: "content_entity",
					Payload: map[string]any{
						"repo_id":     "repo-123",
						"entity_id":   "content-entity:e_tbl1",
						"entity_type": "SqlTable",
						"entity_name": "public.users",
					},
				},
				{
					FactKind: "content_entity",
					Payload: map[string]any{
						"repo_id":     "repo-123",
						"entity_id":   "content-entity:e_view1",
						"entity_type": "SqlView",
						"entity_name": "public.active_users",
						"entity_metadata": map[string]any{
							"source_tables":   []any{"public.users"},
							"sql_entity_type": "SqlView",
						},
					},
				},
				{
					FactKind: "content_entity",
					Payload: map[string]any{
						"repo_id":     "repo-123",
						"entity_id":   "content-entity:e_col1",
						"entity_type": "SqlColumn",
						"entity_name": "public.users.email",
						"entity_metadata": map[string]any{
							"table_name":      "public.users",
							"sql_entity_type": "SqlColumn",
						},
					},
				},
				{
					FactKind: "content_entity",
					Payload: map[string]any{
						"repo_id":     "repo-123",
						"entity_id":   "content-entity:e_trig1",
						"entity_type": "SqlTrigger",
						"entity_name": "users_touch",
						"entity_metadata": map[string]any{
							"table_name":      "public.users",
							"sql_entity_type": "SqlTrigger",
						},
					},
				},
			},
		},
		EdgeWriter: writer,
	}

	intent := Intent{
		IntentID:        "intent-sql-rel-1",
		ScopeID:         "scope-db",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainSQLRelationshipMaterialization,
		Cause:           "sql relationship follow-up required",
		EntityKeys:      []string{"repo-123"},
		RelatedScopeIDs: []string{"scope-db"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusPending,
	}

	result, err := handler.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("result.Status = %q, want %q", result.Status, ResultStatusSucceeded)
	}
	if result.CanonicalWrites != 3 {
		t.Fatalf("result.CanonicalWrites = %d, want 3", result.CanonicalWrites)
	}

	if writer.retractDomain != DomainSQLRelationships {
		t.Fatalf("retractDomain = %q, want %q", writer.retractDomain, DomainSQLRelationships)
	}
	if writer.retractEvidenceSource != sqlRelationshipEvidenceSource {
		t.Fatalf("retractEvidenceSource = %q, want %q", writer.retractEvidenceSource, sqlRelationshipEvidenceSource)
	}
	if writer.writeDomain != DomainSQLRelationships {
		t.Fatalf("writeDomain = %q, want %q", writer.writeDomain, DomainSQLRelationships)
	}
	if writer.writeEvidenceSource != sqlRelationshipEvidenceSource {
		t.Fatalf("writeEvidenceSource = %q, want %q", writer.writeEvidenceSource, sqlRelationshipEvidenceSource)
	}
	if len(writer.writeRows) != 3 {
		t.Fatalf("len(writeRows) = %d, want 3", len(writer.writeRows))
	}

	// Rows are sorted; HAS_COLUMN < REFERENCES_TABLE < TRIGGERS
	if got, want := writer.writeRows[0].Payload["relationship_type"], "HAS_COLUMN"; got != want {
		t.Fatalf("writeRows[0].relationship_type = %v, want %v", got, want)
	}
	if got, want := writer.writeRows[1].Payload["relationship_type"], "REFERENCES_TABLE"; got != want {
		t.Fatalf("writeRows[1].relationship_type = %v, want %v", got, want)
	}
	if got, want := writer.writeRows[2].Payload["relationship_type"], "TRIGGERS"; got != want {
		t.Fatalf("writeRows[2].relationship_type = %v, want %v", got, want)
	}
}

func TestSQLRelationshipHandlerLogsStageTiming(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	now := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	handler := SQLRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{
			envelopes: []facts.Envelope{
				{
					FactKind: "content_entity",
					Payload: map[string]any{
						"repo_id":     "repo-123",
						"entity_id":   "content-entity:e_tbl1",
						"entity_type": "SqlTable",
						"entity_name": "public.users",
					},
				},
				{
					FactKind: "content_entity",
					Payload: map[string]any{
						"repo_id":     "repo-123",
						"entity_id":   "content-entity:e_view1",
						"entity_type": "SqlView",
						"entity_name": "public.active_users",
						"entity_metadata": map[string]any{
							"source_tables": []any{"public.users"},
						},
					},
				},
			},
		},
		EdgeWriter: &recordingSQLRelEdgeWriter{},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-sql-rel-1",
		ScopeID:      "scope-db",
		GenerationID: "gen-1",
		SourceSystem: "git",
		Domain:       DomainSQLRelationshipMaterialization,
		EnqueuedAt:   now,
		AvailableAt:  now,
		Status:       IntentStatusPending,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	logText := logs.String()
	for _, want := range []string{
		`"msg":"sql relationship materialization completed"`,
		`"fact_count":2`,
		`"repo_count":1`,
		`"edge_count":1`,
		`"write_row_count":1`,
		`"load_facts_duration_seconds":`,
		`"extract_duration_seconds":`,
		`"retract_duration_seconds":`,
		`"build_write_rows_duration_seconds":`,
		`"graph_write_duration_seconds":`,
		`"total_duration_seconds":`,
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("logs missing %s:\n%s", want, logText)
		}
	}
}

func TestSQLRelationshipHandlerSkipsFirstGenerationRetract(t *testing.T) {
	t.Parallel()

	writer := &recordingSQLRelEdgeWriter{}
	handler := SQLRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: sqlRelationshipEntityFacts()},
		EdgeWriter: writer,
		PriorGenerationCheck: func(_ context.Context, scopeID, generationID string) (bool, error) {
			if scopeID != "scope-db" || generationID != "gen-1" {
				t.Fatalf("PriorGenerationCheck(%q, %q), want scope-db/gen-1", scopeID, generationID)
			}
			return false, nil
		},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-sql-rel-1",
		ScopeID:      "scope-db",
		GenerationID: "gen-1",
		SourceSystem: "git",
		Domain:       DomainSQLRelationshipMaterialization,
		EnqueuedAt:   time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC),
		AvailableAt:  time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC),
		Status:       IntentStatusPending,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if writer.retractDomain != "" || len(writer.retractRows) != 0 {
		t.Fatalf("retract = domain %q rows %d, want skipped", writer.retractDomain, len(writer.retractRows))
	}
	if len(writer.writeRows) == 0 {
		t.Fatal("writeRows is empty, want SQL edges still written")
	}
}

func TestSQLRelationshipHandlerRetractsWhenPriorGenerationExists(t *testing.T) {
	t.Parallel()

	writer := &recordingSQLRelEdgeWriter{}
	handler := SQLRelationshipMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: sqlRelationshipEntityFacts()},
		EdgeWriter: writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) {
			return true, nil
		},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-sql-rel-1",
		ScopeID:      "scope-db",
		GenerationID: "gen-1",
		SourceSystem: "git",
		Domain:       DomainSQLRelationshipMaterialization,
		EnqueuedAt:   time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC),
		AvailableAt:  time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC),
		Status:       IntentStatusPending,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if writer.retractDomain != DomainSQLRelationships {
		t.Fatalf("retractDomain = %q, want %q", writer.retractDomain, DomainSQLRelationships)
	}
}

// --- test stubs ---

func sqlRelationshipEntityFacts() []facts.Envelope {
	return []facts.Envelope{
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-123",
				"entity_id":   "content-entity:e_tbl1",
				"entity_type": "SqlTable",
				"entity_name": "public.users",
			},
		},
		{
			FactKind: "content_entity",
			Payload: map[string]any{
				"repo_id":     "repo-123",
				"entity_id":   "content-entity:e_view1",
				"entity_type": "SqlView",
				"entity_name": "public.active_users",
				"entity_metadata": map[string]any{
					"source_tables": []any{"public.users"},
				},
			},
		},
	}
}

type recordingSQLRelEdgeWriter struct {
	retractDomain         string
	retractEvidenceSource string
	retractRows           []SharedProjectionIntentRow
	writeDomain           string
	writeEvidenceSource   string
	writeRows             []SharedProjectionIntentRow
}

func (r *recordingSQLRelEdgeWriter) RetractEdges(
	_ context.Context,
	domain string,
	rows []SharedProjectionIntentRow,
	evidenceSource string,
) error {
	r.retractDomain = domain
	r.retractEvidenceSource = evidenceSource
	r.retractRows = append(r.retractRows, rows...)
	return nil
}

func (r *recordingSQLRelEdgeWriter) WriteEdges(
	_ context.Context,
	domain string,
	rows []SharedProjectionIntentRow,
	evidenceSource string,
) error {
	r.writeDomain = domain
	r.writeEvidenceSource = evidenceSource
	r.writeRows = append(r.writeRows, rows...)
	return nil
}
