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
)

func TestAdmissionDecisionSchemaSQL(t *testing.T) {
	t.Parallel()

	sqlStr := AdmissionDecisionSchemaSQL()
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS admission_decisions",
		"CREATE TABLE IF NOT EXISTS admission_decision_evidence",
		"admission_decisions_scope_generation_domain_idx",
		"admission_decisions_anchor_idx",
		"admission_decision_evidence_decision_idx",
		"source_handles JSONB NOT NULL",
		"canonical_write JSONB NOT NULL",
		"recommended_action JSONB NOT NULL",
		"CHECK (state IN ('admitted', 'rejected', 'ambiguous', 'stale', 'missing_evidence', 'permission_hidden', 'unsupported', 'unsafe'))",
	} {
		if !strings.Contains(sqlStr, want) {
			t.Fatalf("AdmissionDecisionSchemaSQL() missing %q", want)
		}
	}
}

func TestAdmissionDecisionStatesCoverClosedVocabulary(t *testing.T) {
	t.Parallel()

	got := make([]string, 0)
	for _, state := range AdmissionDecisionStateValues() {
		got = append(got, string(state))
	}
	sort.Strings(got)
	want := []string{
		"admitted",
		"ambiguous",
		"missing_evidence",
		"permission_hidden",
		"rejected",
		"stale",
		"unsafe",
		"unsupported",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("states = %v, want %v", got, want)
	}
}

func TestAdmissionDecisionStoreUpsertListAndEvidence(t *testing.T) {
	t.Parallel()

	db := newAdmissionDecisionTestDB()
	store := NewAdmissionDecisionStore(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	decision := AdmissionDecision{
		DecisionID:       "admission:deployable-unit:repo-a",
		Domain:           "deployable_unit_correlation",
		State:            AdmissionDecisionStateAmbiguous,
		DomainState:      "ambiguous",
		ScopeID:          "scope:repo-a",
		GenerationID:     "gen-a",
		AnchorKind:       "repository",
		AnchorID:         "repository:repo-a",
		CandidateKind:    "service",
		CandidateID:      "service:payments",
		ConfidenceScore:  0.42,
		ConfidenceBucket: "medium",
		ConfidenceBasis:  "multiple service candidates share the repository",
		FreshnessState:   "current",
		FreshnessCause:   "active_generation",
		SourceHandles: []AdmissionDecisionSourceHandle{
			{Kind: "fact", ID: "fact:repo-a", ScopeID: "scope:repo-a"},
			{Kind: "projection_decision", ID: "projection:repo-a"},
		},
		RedactionState: "safe",
		CanonicalWrite: AdmissionDecisionCanonicalWrite{
			Eligible:      false,
			Written:       false,
			TargetKind:    "relationship",
			SkippedReason: "ambiguous",
		},
		RecommendedAction: AdmissionDecisionNextAction{
			Action: "provide_catalog_owner",
			Reason: "candidate service ownership is unresolved",
		},
		PayloadVersion: "correlation.admission.v1",
		DecidedAt:      now,
		UpdatedAt:      now,
	}

	if err := store.UpsertDecision(ctx, decision); err != nil {
		t.Fatalf("UpsertDecision: %v", err)
	}

	decision.State = AdmissionDecisionStateAdmitted
	decision.DomainState = "exact"
	decision.ConfidenceScore = 0.98
	decision.CanonicalWrite.Eligible = true
	decision.CanonicalWrite.Written = true
	decision.CanonicalWrite.SkippedReason = ""
	if err := store.UpsertDecision(ctx, decision); err != nil {
		t.Fatalf("second UpsertDecision: %v", err)
	}

	rows, err := store.ListDecisions(ctx, AdmissionDecisionFilter{
		Domain:       "deployable_unit_correlation",
		ScopeID:      "scope:repo-a",
		GenerationID: "gen-a",
		Limit:        100,
	})
	if err != nil {
		t.Fatalf("ListDecisions: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	got := rows[0]
	if got.State != AdmissionDecisionStateAdmitted {
		t.Fatalf("state = %q, want admitted after overwrite", got.State)
	}
	if got.ConfidenceScore != 0.98 {
		t.Fatalf("confidence = %f, want 0.98", got.ConfidenceScore)
	}
	if !got.CanonicalWrite.Written {
		t.Fatal("canonical write posture was not preserved")
	}
	if len(got.SourceHandles) != 2 {
		t.Fatalf("source handles = %d, want 2", len(got.SourceHandles))
	}

	evidence := []AdmissionDecisionEvidence{
		{
			EvidenceID:   "admission-evidence:1",
			DecisionID:   decision.DecisionID,
			SourceHandle: "fact:repo-a",
			EvidenceKind: "input_fact",
			Detail:       map[string]any{"fact_kind": "reducer_service_catalog_correlation"},
			CreatedAt:    now,
		},
	}
	if err := store.InsertEvidence(ctx, evidence); err != nil {
		t.Fatalf("InsertEvidence: %v", err)
	}
	gotEvidence, err := store.ListEvidence(ctx, decision.DecisionID)
	if err != nil {
		t.Fatalf("ListEvidence: %v", err)
	}
	if len(gotEvidence) != 1 {
		t.Fatalf("len(evidence) = %d, want 1", len(gotEvidence))
	}
	if gotEvidence[0].SourceHandle != "fact:repo-a" {
		t.Fatalf("source handle = %q, want fact:repo-a", gotEvidence[0].SourceHandle)
	}
	if gotEvidence[0].Detail["fact_kind"] != "reducer_service_catalog_correlation" {
		t.Fatalf("detail = %#v", gotEvidence[0].Detail)
	}
}

func TestAdmissionDecisionStoreRequiresBoundedListFilter(t *testing.T) {
	t.Parallel()

	store := NewAdmissionDecisionStore(newAdmissionDecisionTestDB())
	_, err := store.ListDecisions(context.Background(), AdmissionDecisionFilter{
		Domain:       "deployable_unit_correlation",
		ScopeID:      "scope:repo-a",
		GenerationID: "",
		Limit:        100,
	})
	if err == nil {
		t.Fatal("ListDecisions() error = nil, want bounded filter error")
	}
}

func TestAdmissionDecisionStoreClampsListLimit(t *testing.T) {
	t.Parallel()

	db := newAdmissionDecisionTestDB()
	store := NewAdmissionDecisionStore(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	for i := range 3 {
		decision := AdmissionDecision{
			DecisionID:       fmt.Sprintf("admission:%d", i),
			Domain:           "package_supply_chain",
			State:            AdmissionDecisionStateRejected,
			DomainState:      "rejected",
			ScopeID:          "scope:repo-b",
			GenerationID:     "gen-b",
			AnchorKind:       "package",
			AnchorID:         "package:library",
			CandidateKind:    "repository",
			CandidateID:      fmt.Sprintf("repository:%d", i),
			ConfidenceScore:  0.1,
			ConfidenceBucket: "low",
			ConfidenceBasis:  "no ownership evidence",
			FreshnessState:   "current",
			FreshnessCause:   "active_generation",
			RedactionState:   "safe",
			CanonicalWrite: AdmissionDecisionCanonicalWrite{
				Eligible:      false,
				Written:       false,
				SkippedReason: "rejected",
			},
			RecommendedAction: AdmissionDecisionNextAction{Action: "add_package_owner"},
			PayloadVersion:    "correlation.admission.v1",
			DecidedAt:         now.Add(time.Duration(i) * time.Minute),
			UpdatedAt:         now.Add(time.Duration(i) * time.Minute),
		}
		if err := store.UpsertDecision(ctx, decision); err != nil {
			t.Fatalf("UpsertDecision(%d): %v", i, err)
		}
	}

	rows, err := store.ListDecisions(ctx, AdmissionDecisionFilter{
		Domain:       "package_supply_chain",
		ScopeID:      "scope:repo-b",
		GenerationID: "gen-b",
		Limit:        0,
	})
	if err != nil {
		t.Fatalf("ListDecisions: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want default bounded page of 1", len(rows))
	}
	if rows[0].DecisionID != "admission:2" {
		t.Fatalf("first row = %q, want newest admission:2", rows[0].DecisionID)
	}
}

type admissionDecisionTestDB struct {
	decisions map[string]AdmissionDecision
	evidence  map[string]AdmissionDecisionEvidence
}

func newAdmissionDecisionTestDB() *admissionDecisionTestDB {
	return &admissionDecisionTestDB{
		decisions: make(map[string]AdmissionDecision),
		evidence:  make(map[string]AdmissionDecisionEvidence),
	}
}

func (db *admissionDecisionTestDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	switch {
	case strings.Contains(query, "INSERT INTO admission_decisions"):
		decision := AdmissionDecision{
			DecisionID:       args[0].(string),
			Domain:           args[1].(string),
			State:            AdmissionDecisionState(args[2].(string)),
			DomainState:      args[3].(string),
			ScopeID:          args[4].(string),
			GenerationID:     args[5].(string),
			AnchorKind:       args[6].(string),
			AnchorID:         args[7].(string),
			CandidateKind:    args[8].(string),
			CandidateID:      args[9].(string),
			ConfidenceScore:  args[10].(float64),
			ConfidenceBucket: args[11].(string),
			ConfidenceBasis:  args[12].(string),
			FreshnessState:   args[13].(string),
			FreshnessCause:   args[15].(string),
			RedactionState:   args[17].(string),
			RedactionReason:  args[18].(string),
			PayloadVersion:   args[21].(string),
			DecidedAt:        args[22].(time.Time),
			UpdatedAt:        args[23].(time.Time),
		}
		if observedAt, ok := args[14].(time.Time); ok {
			decision.FreshnessObservedAt = &observedAt
		}
		mustUnmarshalAdmissionDecisionTestJSON(args[16], &decision.SourceHandles)
		mustUnmarshalAdmissionDecisionTestJSON(args[19], &decision.CanonicalWrite)
		mustUnmarshalAdmissionDecisionTestJSON(args[20], &decision.RecommendedAction)
		db.decisions[decision.DecisionID] = decision
		return result{}, nil

	case strings.Contains(query, "INSERT INTO admission_decision_evidence"):
		row := AdmissionDecisionEvidence{
			EvidenceID:   args[0].(string),
			DecisionID:   args[1].(string),
			SourceHandle: args[2].(string),
			EvidenceKind: args[3].(string),
			CreatedAt:    args[5].(time.Time),
		}
		mustUnmarshalAdmissionDecisionTestJSON(args[4], &row.Detail)
		db.evidence[row.EvidenceID] = row
		return result{}, nil

	case strings.Contains(query, "CREATE TABLE"):
		return result{}, nil

	default:
		return nil, fmt.Errorf("unexpected exec query: %s", query)
	}
}

func (db *admissionDecisionTestDB) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	switch {
	case strings.Contains(query, "FROM admission_decisions"):
		domain := args[0].(string)
		scopeID := args[1].(string)
		generationID := args[2].(string)
		state := args[3].(string)
		anchorKind := args[4].(string)
		anchorID := args[5].(string)
		limit := args[6].(int)
		rows := make([]AdmissionDecision, 0)
		for _, decision := range db.decisions {
			if decision.Domain != domain || decision.ScopeID != scopeID || decision.GenerationID != generationID {
				continue
			}
			if state != "" && string(decision.State) != state {
				continue
			}
			if anchorKind != "" && decision.AnchorKind != anchorKind {
				continue
			}
			if anchorID != "" && decision.AnchorID != anchorID {
				continue
			}
			rows = append(rows, decision)
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].UpdatedAt.Equal(rows[j].UpdatedAt) {
				return rows[i].DecisionID < rows[j].DecisionID
			}
			return rows[i].UpdatedAt.After(rows[j].UpdatedAt)
		})
		if len(rows) > limit {
			rows = rows[:limit]
		}
		return admissionDecisionRowsFromDecisions(rows), nil

	case strings.Contains(query, "FROM admission_decision_evidence"):
		decisionID := args[0].(string)
		rows := make([]AdmissionDecisionEvidence, 0)
		for _, evidence := range db.evidence {
			if evidence.DecisionID == decisionID {
				rows = append(rows, evidence)
			}
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
				return rows[i].EvidenceID < rows[j].EvidenceID
			}
			return rows[i].CreatedAt.Before(rows[j].CreatedAt)
		})
		return admissionDecisionRowsFromEvidence(rows), nil

	default:
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
}

func mustUnmarshalAdmissionDecisionTestJSON(arg any, dest any) {
	bytes, ok := arg.([]byte)
	if !ok || len(bytes) == 0 {
		return
	}
	if err := json.Unmarshal(bytes, dest); err != nil {
		panic(err)
	}
}

func admissionDecisionRowsFromDecisions(rows []AdmissionDecision) Rows {
	data := make([][]any, 0, len(rows))
	for _, decision := range rows {
		sourceHandles, _ := json.Marshal(decision.SourceHandles)
		canonicalWrite, _ := json.Marshal(decision.CanonicalWrite)
		nextAction, _ := json.Marshal(decision.RecommendedAction)
		var observedAt any
		if decision.FreshnessObservedAt != nil {
			observedAt = *decision.FreshnessObservedAt
		}
		data = append(data, []any{
			decision.DecisionID,
			decision.Domain,
			string(decision.State),
			decision.DomainState,
			decision.ScopeID,
			decision.GenerationID,
			decision.AnchorKind,
			decision.AnchorID,
			decision.CandidateKind,
			decision.CandidateID,
			decision.ConfidenceScore,
			decision.ConfidenceBucket,
			decision.ConfidenceBasis,
			decision.FreshnessState,
			observedAt,
			decision.FreshnessCause,
			sourceHandles,
			decision.RedactionState,
			decision.RedactionReason,
			canonicalWrite,
			nextAction,
			decision.PayloadVersion,
			decision.DecidedAt,
			decision.UpdatedAt,
		})
	}
	return &admissionDecisionRows{data: data, idx: -1}
}

func admissionDecisionRowsFromEvidence(rows []AdmissionDecisionEvidence) Rows {
	data := make([][]any, 0, len(rows))
	for _, row := range rows {
		detail, _ := json.Marshal(row.Detail)
		data = append(data, []any{
			row.EvidenceID,
			row.DecisionID,
			row.SourceHandle,
			row.EvidenceKind,
			detail,
			row.CreatedAt,
		})
	}
	return &admissionDecisionRows{data: data, idx: -1}
}

type admissionDecisionRows struct {
	data [][]any
	idx  int
}

func (r *admissionDecisionRows) Next() bool {
	r.idx++
	return r.idx < len(r.data)
}

func (r *admissionDecisionRows) Scan(dest ...any) error {
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
				continue
			}
			*d = val.(string)
		case *float64:
			*d = val.(float64)
		case *time.Time:
			*d = val.(time.Time)
		case *sql.NullTime:
			if val == nil {
				*d = sql.NullTime{}
				continue
			}
			*d = sql.NullTime{Time: val.(time.Time), Valid: true}
		case **time.Time:
			if val == nil {
				*d = nil
				continue
			}
			tm := val.(time.Time)
			*d = &tm
		case *[]byte:
			*d = val.([]byte)
		default:
			return fmt.Errorf("unsupported scan dest type %T", dest[i])
		}
	}
	return nil
}

func (r *admissionDecisionRows) Err() error {
	return nil
}

func (r *admissionDecisionRows) Close() error {
	return nil
}
