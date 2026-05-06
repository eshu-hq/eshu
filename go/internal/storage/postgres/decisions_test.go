package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

func TestDecisionStoreUpsertAndList(t *testing.T) {
	t.Parallel()

	db := newDecisionTestDB()
	store := NewDecisionStore(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Microsecond)
	d := projector.ProjectionDecisionRow{
		DecisionID:        "d-1",
		DecisionType:      "project_workloads",
		RepositoryID:      "repository:r_payments",
		SourceRunID:       "run-001",
		WorkItemID:        "wi-001",
		Subject:           "repository:r_payments",
		ConfidenceScore:   0.9,
		ConfidenceReason:  "high confidence",
		ProvenanceSummary: map[string]any{"fact_count": 3},
		CreatedAt:         now,
	}

	if err := store.UpsertDecision(ctx, d); err != nil {
		t.Fatalf("UpsertDecision: %v", err)
	}

	rows, err := store.ListDecisions(ctx, DecisionFilter{
		RepositoryID: "repository:r_payments",
		SourceRunID:  "run-001",
		Limit:        100,
	})
	if err != nil {
		t.Fatalf("ListDecisions: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len = %d, want 1", len(rows))
	}
	if rows[0].DecisionID != "d-1" {
		t.Errorf("DecisionID = %q", rows[0].DecisionID)
	}
	if rows[0].ConfidenceScore != 0.9 {
		t.Errorf("ConfidenceScore = %f", rows[0].ConfidenceScore)
	}
}

func TestDecisionStoreUpsertOverwrites(t *testing.T) {
	t.Parallel()

	db := newDecisionTestDB()
	store := NewDecisionStore(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Microsecond)
	d := projector.ProjectionDecisionRow{
		DecisionID:        "d-upsert",
		DecisionType:      "project_workloads",
		RepositoryID:      "repository:r_api",
		SourceRunID:       "run-002",
		WorkItemID:        "wi-002",
		Subject:           "repository:r_api",
		ConfidenceScore:   0.6,
		ConfidenceReason:  "initial",
		ProvenanceSummary: map[string]any{},
		CreatedAt:         now,
	}

	if err := store.UpsertDecision(ctx, d); err != nil {
		t.Fatalf("first UpsertDecision: %v", err)
	}

	d.ConfidenceScore = 0.9
	d.ConfidenceReason = "updated"
	if err := store.UpsertDecision(ctx, d); err != nil {
		t.Fatalf("second UpsertDecision: %v", err)
	}

	rows, err := store.ListDecisions(ctx, DecisionFilter{
		RepositoryID: "repository:r_api",
		SourceRunID:  "run-002",
		Limit:        100,
	})
	if err != nil {
		t.Fatalf("ListDecisions: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len = %d, want 1", len(rows))
	}
	if rows[0].ConfidenceScore != 0.9 {
		t.Errorf("ConfidenceScore = %f, want 0.9 after upsert", rows[0].ConfidenceScore)
	}
	if rows[0].ConfidenceReason != "updated" {
		t.Errorf("ConfidenceReason = %q, want updated", rows[0].ConfidenceReason)
	}
}

func TestDecisionStoreFilterByType(t *testing.T) {
	t.Parallel()

	db := newDecisionTestDB()
	store := NewDecisionStore(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, dt := range []string{"project_workloads", "project_entities"} {
		d := projector.ProjectionDecisionRow{
			DecisionID:        "d-filter-" + dt,
			DecisionType:      dt,
			RepositoryID:      "repository:r_filter",
			SourceRunID:       "run-filter",
			WorkItemID:        "wi-filter-" + dt,
			Subject:           "repository:r_filter",
			ConfidenceScore:   0.7,
			ConfidenceReason:  "test",
			ProvenanceSummary: map[string]any{},
			CreatedAt:         now,
		}
		if err := store.UpsertDecision(ctx, d); err != nil {
			t.Fatalf("UpsertDecision(%s): %v", dt, err)
		}
	}

	dtype := "project_workloads"
	rows, err := store.ListDecisions(ctx, DecisionFilter{
		RepositoryID: "repository:r_filter",
		SourceRunID:  "run-filter",
		DecisionType: &dtype,
		Limit:        100,
	})
	if err != nil {
		t.Fatalf("ListDecisions: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len = %d, want 1", len(rows))
	}
	if rows[0].DecisionType != "project_workloads" {
		t.Errorf("DecisionType = %q", rows[0].DecisionType)
	}
}

func TestDecisionStoreEvidenceInsertAndList(t *testing.T) {
	t.Parallel()

	db := newDecisionTestDB()
	store := NewDecisionStore(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Microsecond)
	factID := "fact-1"
	evidence := []projector.ProjectionDecisionEvidenceRow{
		{
			EvidenceID:   "ev-1",
			DecisionID:   "d-ev-test",
			FactID:       &factID,
			EvidenceKind: "input",
			Detail:       map[string]any{"fact_type": "file_fact"},
			CreatedAt:    now,
		},
		{
			EvidenceID:   "ev-2",
			DecisionID:   "d-ev-test",
			FactID:       nil,
			EvidenceKind: "input",
			Detail:       map[string]any{"fact_type": "entity_fact"},
			CreatedAt:    now,
		},
	}

	if err := store.InsertEvidence(ctx, evidence); err != nil {
		t.Fatalf("InsertEvidence: %v", err)
	}

	rows, err := store.ListEvidence(ctx, "d-ev-test")
	if err != nil {
		t.Fatalf("ListEvidence: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len = %d, want 2", len(rows))
	}
	if *rows[0].FactID != "fact-1" {
		t.Errorf("FactID = %q", *rows[0].FactID)
	}
	if rows[1].FactID != nil {
		t.Errorf("FactID should be nil for ev-2")
	}
}

func TestDecisionStoreEmptyEvidenceInsert(t *testing.T) {
	t.Parallel()

	db := newDecisionTestDB()
	store := NewDecisionStore(db)
	ctx := context.Background()

	// Should be a no-op, not an error.
	if err := store.InsertEvidence(ctx, nil); err != nil {
		t.Fatalf("InsertEvidence(nil): %v", err)
	}
}

func TestDecisionStoreListDecisionsDefaultLimit(t *testing.T) {
	t.Parallel()

	db := newDecisionTestDB()
	store := NewDecisionStore(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Microsecond)
	// Insert 3 decisions, request limit=0 should clamp to 1.
	for i := range 3 {
		d := projector.ProjectionDecisionRow{
			DecisionID:        fmt.Sprintf("d-limit-%d", i),
			DecisionType:      "project_entities",
			RepositoryID:      "repository:r_limit",
			SourceRunID:       "run-limit",
			WorkItemID:        fmt.Sprintf("wi-limit-%d", i),
			Subject:           "repository:r_limit",
			ConfidenceScore:   0.6,
			ConfidenceReason:  "test",
			ProvenanceSummary: map[string]any{},
			CreatedAt:         now.Add(time.Duration(i) * time.Second),
		}
		if err := store.UpsertDecision(ctx, d); err != nil {
			t.Fatalf("UpsertDecision: %v", err)
		}
	}

	rows, err := store.ListDecisions(ctx, DecisionFilter{
		RepositoryID: "repository:r_limit",
		SourceRunID:  "run-limit",
		Limit:        0,
	})
	if err != nil {
		t.Fatalf("ListDecisions: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("len = %d, want 1 (limit clamped to 1)", len(rows))
	}
}

func TestDecisionStoreSchemaSQL(t *testing.T) {
	t.Parallel()

	sql := DecisionSchemaSQL()
	if !strings.Contains(sql, "CREATE TABLE IF NOT EXISTS projection_decisions") {
		t.Error("missing projection_decisions table")
	}
	if !strings.Contains(sql, "CREATE TABLE IF NOT EXISTS projection_decision_evidence") {
		t.Error("missing projection_decision_evidence table")
	}
	if !strings.Contains(sql, "projection_decisions_repo_run_idx") {
		t.Error("missing repo_run index")
	}
	if !strings.Contains(sql, "projection_decision_evidence_decision_idx") {
		t.Error("missing evidence decision index")
	}
}

// -- test helpers --

// decisionTestDB is an in-memory mock of ExecQueryer that stores decisions
// and evidence for unit testing. It follows the same pattern as proofDomainDB.
type decisionTestDB struct {
	decisions map[string]projector.ProjectionDecisionRow
	evidence  map[string]projector.ProjectionDecisionEvidenceRow
}

func newDecisionTestDB() *decisionTestDB {
	return &decisionTestDB{
		decisions: make(map[string]projector.ProjectionDecisionRow),
		evidence:  make(map[string]projector.ProjectionDecisionEvidenceRow),
	}
}

func (db *decisionTestDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	switch {
	case strings.Contains(query, "INSERT INTO projection_decisions"):
		row := projector.ProjectionDecisionRow{
			DecisionID:       args[0].(string),
			DecisionType:     args[1].(string),
			RepositoryID:     args[2].(string),
			SourceRunID:      args[3].(string),
			WorkItemID:       args[4].(string),
			Subject:          args[5].(string),
			ConfidenceScore:  args[6].(float64),
			ConfidenceReason: args[7].(string),
			CreatedAt:        args[9].(time.Time),
		}
		if b, ok := args[8].([]byte); ok {
			var m map[string]any
			if err := json.Unmarshal(b, &m); err == nil {
				row.ProvenanceSummary = m
			}
		}
		db.decisions[row.DecisionID] = row
		return proofResult{}, nil

	case strings.Contains(query, "INSERT INTO projection_decision_evidence"):
		factID := stringPtrFromAny(args[2])
		row := projector.ProjectionDecisionEvidenceRow{
			EvidenceID:   args[0].(string),
			DecisionID:   args[1].(string),
			FactID:       factID,
			EvidenceKind: args[3].(string),
			CreatedAt:    args[5].(time.Time),
		}
		if b, ok := args[4].([]byte); ok {
			var m map[string]any
			if err := json.Unmarshal(b, &m); err == nil {
				row.Detail = m
			}
		}
		db.evidence[row.EvidenceID] = row
		return proofResult{}, nil

	default:
		return nil, fmt.Errorf("unexpected exec query: %s", query)
	}
}

func (db *decisionTestDB) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	switch {
	case strings.Contains(query, "FROM projection_decisions"):
		repoID := args[0].(string)
		runID := args[1].(string)
		var decisionType *string
		if s, ok := args[2].(string); ok && s != "" {
			decisionType = &s
		}
		limit := args[3].(int)
		if limit < 1 {
			limit = 1
		}

		var rows [][]any
		for _, d := range db.decisions {
			if d.RepositoryID != repoID || d.SourceRunID != runID {
				continue
			}
			if decisionType != nil && d.DecisionType != *decisionType {
				continue
			}
			provBytes, _ := json.Marshal(d.ProvenanceSummary)
			rows = append(rows, []any{
				d.DecisionID,
				d.DecisionType,
				d.RepositoryID,
				d.SourceRunID,
				d.WorkItemID,
				d.Subject,
				d.ConfidenceScore,
				d.ConfidenceReason,
				provBytes,
				d.CreatedAt,
			})
			if len(rows) >= limit {
				break
			}
		}
		return newDecisionRows(rows), nil

	case strings.Contains(query, "FROM projection_decision_evidence"):
		decisionID := args[0].(string)
		// Collect matching evidence, then sort by (created_at, evidence_id) to
		// match the real SQL ORDER BY and avoid map-iteration non-determinism.
		var matched []projector.ProjectionDecisionEvidenceRow
		for _, e := range db.evidence {
			if e.DecisionID != decisionID {
				continue
			}
			matched = append(matched, e)
		}
		sort.Slice(matched, func(i, j int) bool {
			if !matched[i].CreatedAt.Equal(matched[j].CreatedAt) {
				return matched[i].CreatedAt.Before(matched[j].CreatedAt)
			}
			return matched[i].EvidenceID < matched[j].EvidenceID
		})
		var rows [][]any
		for _, e := range matched {
			detailBytes, _ := json.Marshal(e.Detail)
			var factID any
			if e.FactID != nil {
				factID = *e.FactID
			}
			rows = append(rows, []any{
				e.EvidenceID,
				e.DecisionID,
				factID,
				e.EvidenceKind,
				detailBytes,
				e.CreatedAt,
			})
		}
		return newDecisionRows(rows), nil

	default:
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
}

func stringPtrFromAny(v any) *string {
	if v == nil {
		return nil
	}
	s, ok := v.(string)
	if !ok {
		return nil
	}
	return &s
}

// decisionRows implements the Rows interface for decision test queries.
type decisionRows struct {
	data [][]any
	idx  int
}

func newDecisionRows(data [][]any) *decisionRows {
	return &decisionRows{data: data, idx: -1}
}

func (r *decisionRows) Next() bool {
	r.idx++
	return r.idx < len(r.data)
}

func (r *decisionRows) Scan(dest ...any) error {
	if r.idx < 0 || r.idx >= len(r.data) {
		return fmt.Errorf("scan out of range")
	}
	row := r.data[r.idx]
	if len(dest) != len(row) {
		return fmt.Errorf("scan: got %d dest, have %d cols", len(dest), len(row))
	}
	for i, val := range row {
		switch d := dest[i].(type) {
		case *string:
			if val == nil {
				*d = ""
			} else {
				*d = val.(string)
			}
		case *float64:
			*d = val.(float64)
		case *int:
			*d = val.(int)
		case *time.Time:
			*d = val.(time.Time)
		case *[]byte:
			if b, ok := val.([]byte); ok {
				*d = b
			}
		case *sql.NullString:
			if val == nil {
				d.Valid = false
			} else {
				d.String = val.(string)
				d.Valid = true
			}
		default:
			return fmt.Errorf("unsupported scan dest type %T", dest[i])
		}
	}
	return nil
}

func (r *decisionRows) Err() error   { return nil }
func (r *decisionRows) Close() error { return nil }
