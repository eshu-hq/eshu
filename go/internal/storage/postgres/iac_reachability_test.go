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

func TestIaCReachabilityStoreUpsertAndListCleanupFindings(t *testing.T) {
	t.Parallel()

	db := newIaCReachabilityTestDB()
	store := NewIaCReachabilityStore(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	rows := []IaCReachabilityRow{
		{
			ScopeID:      "scope-1",
			GenerationID: "gen-1",
			RepoID:       "terraform-modules",
			Family:       "terraform",
			ArtifactPath: "modules/checkout-service",
			ArtifactName: "checkout-service",
			Reachability: IaCReachabilityUsed,
			Finding:      IaCFindingInUse,
			Confidence:   0.99,
			Evidence:     []string{"terraform-stack/main.tf: source modules/checkout-service"},
			ObservedAt:   now,
			UpdatedAt:    now,
		},
		{
			ScopeID:      "scope-1",
			GenerationID: "gen-1",
			RepoID:       "terraform-modules",
			Family:       "terraform",
			ArtifactPath: "modules/orphan-cache",
			ArtifactName: "orphan-cache",
			Reachability: IaCReachabilityUnused,
			Finding:      IaCFindingCandidateDead,
			Confidence:   0.75,
			Evidence:     []string{"module directory exists"},
			ObservedAt:   now,
			UpdatedAt:    now,
		},
		{
			ScopeID:      "scope-1",
			GenerationID: "gen-1",
			RepoID:       "terraform-modules",
			Family:       "terraform",
			ArtifactPath: "modules/dynamic-target",
			ArtifactName: "dynamic-target",
			Reachability: IaCReachabilityAmbiguous,
			Finding:      IaCFindingAmbiguousDynamic,
			Confidence:   0.40,
			Evidence:     []string{"terraform-stack/main.tf: dynamic source"},
			Limitations:  []string{"dynamic reference requires renderer evidence"},
			ObservedAt:   now,
			UpdatedAt:    now,
		},
	}

	if err := store.Upsert(ctx, rows); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := store.ListCleanupFindings(ctx, "scope-1", "gen-1", true, 100, 0)
	if err != nil {
		t.Fatalf("ListCleanupFindings: %v", err)
	}
	if gotLen, wantLen := len(got), 2; gotLen != wantLen {
		t.Fatalf("len = %d, want %d: %#v", gotLen, wantLen, got)
	}
	if got[0].ArtifactName != "dynamic-target" {
		t.Fatalf("first artifact = %q, want dynamic-target", got[0].ArtifactName)
	}
	if got[0].Reachability != IaCReachabilityAmbiguous {
		t.Fatalf("first reachability = %q, want %q", got[0].Reachability, IaCReachabilityAmbiguous)
	}
	if got[1].ArtifactName != "orphan-cache" {
		t.Fatalf("second artifact = %q, want orphan-cache", got[1].ArtifactName)
	}

	withoutAmbiguous, err := store.ListCleanupFindings(ctx, "scope-1", "gen-1", false, 100, 0)
	if err != nil {
		t.Fatalf("ListCleanupFindings without ambiguous: %v", err)
	}
	if gotLen, wantLen := len(withoutAmbiguous), 1; gotLen != wantLen {
		t.Fatalf("len without ambiguous = %d, want %d", gotLen, wantLen)
	}
	if withoutAmbiguous[0].Reachability != IaCReachabilityUnused {
		t.Fatalf("reachability without ambiguous = %q, want %q", withoutAmbiguous[0].Reachability, IaCReachabilityUnused)
	}

	latest, err := store.ListLatestCleanupFindings(ctx, []string{"terraform-modules"}, nil, true, 100, 0)
	if err != nil {
		t.Fatalf("ListLatestCleanupFindings: %v", err)
	}
	if gotLen, wantLen := len(latest), 2; gotLen != wantLen {
		t.Fatalf("latest len = %d, want %d", gotLen, wantLen)
	}
	for _, row := range latest {
		if row.RepoID != "terraform-modules" {
			t.Fatalf("latest repo_id = %q, want terraform-modules", row.RepoID)
		}
		if row.Reachability == IaCReachabilityUsed {
			t.Fatalf("latest returned used row: %#v", row)
		}
	}

	terraformLatest, err := store.ListLatestCleanupFindings(ctx, []string{"terraform-modules"}, []string{"terraform"}, true, 100, 0)
	if err != nil {
		t.Fatalf("ListLatestCleanupFindings terraform: %v", err)
	}
	if gotLen, wantLen := len(terraformLatest), 2; gotLen != wantLen {
		t.Fatalf("terraform latest len = %d, want %d", gotLen, wantLen)
	}
	helmLatest, err := store.ListLatestCleanupFindings(ctx, []string{"terraform-modules"}, []string{"helm"}, true, 100, 0)
	if err != nil {
		t.Fatalf("ListLatestCleanupFindings helm: %v", err)
	}
	if len(helmLatest) != 0 {
		t.Fatalf("helm latest len = %d, want 0", len(helmLatest))
	}
	hasRows, err := store.HasLatestRows(ctx, []string{"terraform-modules"}, []string{"terraform"})
	if err != nil {
		t.Fatalf("HasLatestRows terraform: %v", err)
	}
	if !hasRows {
		t.Fatal("HasLatestRows terraform = false, want true")
	}
	count, err := store.CountLatestCleanupFindings(ctx, []string{"terraform-modules"}, []string{"terraform"}, true)
	if err != nil {
		t.Fatalf("CountLatestCleanupFindings terraform: %v", err)
	}
	if got, want := count, 2; got != want {
		t.Fatalf("CountLatestCleanupFindings terraform = %d, want %d", got, want)
	}
	page, err := store.ListLatestCleanupFindings(ctx, []string{"terraform-modules"}, []string{"terraform"}, true, 1, 1)
	if err != nil {
		t.Fatalf("ListLatestCleanupFindings page: %v", err)
	}
	if got, want := len(page), 1; got != want {
		t.Fatalf("paged len = %d, want %d", got, want)
	}
	if got, want := page[0].ArtifactName, "orphan-cache"; got != want {
		t.Fatalf("paged artifact = %q, want %q", got, want)
	}
}

func TestIaCReachabilitySchemaSQL(t *testing.T) {
	t.Parallel()

	sqlStr := IaCReachabilitySchemaSQL()
	if !strings.Contains(sqlStr, "CREATE TABLE IF NOT EXISTS iac_reachability_rows") {
		t.Fatal("missing iac_reachability_rows table")
	}
	if !strings.Contains(sqlStr, "PRIMARY KEY (scope_id, generation_id, repo_id, family, artifact_path)") {
		t.Fatal("missing durable artifact primary key")
	}
	if !strings.Contains(sqlStr, "iac_reachability_cleanup_idx") {
		t.Fatal("missing cleanup lookup index")
	}
	if !strings.Contains(sqlStr, "evidence JSONB NOT NULL") {
		t.Fatal("missing evidence JSONB column")
	}
}

func TestIngestionStoreMaterializeIaCReachabilityWritesActiveCorpusRows(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 1, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{
				rows: [][]any{
					{"terraform-modules", "scope-modules", "gen-modules"},
					{"terraform-stack", "scope-stack", "gen-stack"},
				},
			},
			{
				rows: [][]any{
					{"terraform-modules", "modules/checkout-service/main.tf", `variable "x" {}`},
					{"terraform-modules", "modules/orphan-cache/main.tf", `variable "x" {}`},
					{"terraform-modules", "modules/dynamic-target/main.tf", `variable "x" {}`},
					{"terraform-stack", "env/main.tf", `
module "checkout" {
  source = "../terraform-modules/modules/checkout-service"
}
module "dynamic" {
  source = "../terraform-modules/modules/${var.module_name}"
}
variable "module_name" {
  default = "dynamic-target"
}
`},
					{"inactive-modules", "modules/ghost/main.tf", `variable "x" {}`},
				},
			},
		},
	}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	if err := store.MaterializeIaCReachability(context.Background(), nil, nil); err != nil {
		t.Fatalf("MaterializeIaCReachability() error = %v, want nil", err)
	}

	inserted := iacReachabilityRowsFromExecs(t, db.execs)
	if got, want := len(inserted), 3; got != want {
		t.Fatalf("materialized row count = %d, want %d: %#v", got, want, inserted)
	}

	byPath := map[string]iacReachabilityStoredRow{}
	for _, row := range inserted {
		byPath[row.ArtifactPath] = row
		if row.ScopeID != "scope-modules" || row.GenerationID != "gen-modules" {
			t.Fatalf("row stored against %s/%s, want module repo active generation: %#v", row.ScopeID, row.GenerationID, row)
		}
		if row.ObservedAt != now || row.UpdatedAt != now {
			t.Fatalf("row timestamps = %s/%s, want %s", row.ObservedAt, row.UpdatedAt, now)
		}
	}

	if got := byPath["modules/checkout-service"].Reachability; got != string(IaCReachabilityUsed) {
		t.Fatalf("checkout reachability = %q, want used", got)
	}
	if got := byPath["modules/orphan-cache"].Reachability; got != string(IaCReachabilityUnused) {
		t.Fatalf("orphan reachability = %q, want unused", got)
	}
	if got := byPath["modules/dynamic-target"].Reachability; got != string(IaCReachabilityAmbiguous) {
		t.Fatalf("dynamic reachability = %q, want ambiguous", got)
	}
	if _, ok := byPath["modules/ghost"]; ok {
		t.Fatal("inactive repository artifact was materialized")
	}
}

type iacReachabilityTestDB struct {
	rows map[string]iacReachabilityStoredRow
}

type iacReachabilityStoredRow struct {
	ScopeID      string
	GenerationID string
	RepoID       string
	Family       string
	ArtifactPath string
	ArtifactName string
	Reachability string
	Finding      string
	Confidence   float64
	Evidence     []string
	Limitations  []string
	ObservedAt   time.Time
	UpdatedAt    time.Time
}

func newIaCReachabilityTestDB() *iacReachabilityTestDB {
	return &iacReachabilityTestDB{rows: map[string]iacReachabilityStoredRow{}}
}

func (db *iacReachabilityTestDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	switch {
	case strings.Contains(query, "INSERT INTO iac_reachability_rows"):
		const columnsPerRow = 13
		for i := 0; i < len(args); i += columnsPerRow {
			evidence, err := decodeStringArrayJSON(args[i+9])
			if err != nil {
				return nil, err
			}
			limitations, err := decodeStringArrayJSON(args[i+10])
			if err != nil {
				return nil, err
			}
			row := iacReachabilityStoredRow{
				ScopeID:      args[i+0].(string),
				GenerationID: args[i+1].(string),
				RepoID:       args[i+2].(string),
				Family:       args[i+3].(string),
				ArtifactPath: args[i+4].(string),
				ArtifactName: args[i+5].(string),
				Reachability: args[i+6].(string),
				Finding:      args[i+7].(string),
				Confidence:   args[i+8].(float64),
				Evidence:     evidence,
				Limitations:  limitations,
				ObservedAt:   args[i+11].(time.Time),
				UpdatedAt:    args[i+12].(time.Time),
			}
			db.rows[iacReachabilityKey(row)] = row
		}
		return sharedIntentResult{}, nil

	case strings.Contains(query, "CREATE TABLE") || strings.Contains(query, "CREATE INDEX"):
		return sharedIntentResult{}, nil

	default:
		return nil, fmt.Errorf("unexpected exec query: %s", query)
	}
}

func (db *iacReachabilityTestDB) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	if !strings.Contains(query, "FROM iac_reachability_rows") {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
	latestActive := strings.Contains(query, "JOIN scope_generations")
	existenceQuery := strings.Contains(query, "SELECT 1")
	countQuery := strings.Contains(query, "SELECT COUNT(*)")
	var scopeID, generationID string
	var repoIDs map[string]struct{}
	var families map[string]struct{}
	var includeAmbiguous bool
	var limit int
	var offset int
	if latestActive {
		repoIDs = map[string]struct{}{}
		families = map[string]struct{}{}
		cleanupArgTail := 0
		switch {
		case countQuery:
			cleanupArgTail = 1
			includeAmbiguous = args[len(args)-1].(bool)
			limit = 100000
		case !existenceQuery:
			cleanupArgTail = 3
			includeAmbiguous = args[len(args)-3].(bool)
			limit = args[len(args)-2].(int)
			offset = args[len(args)-1].(int)
		default:
			limit = 100
		}
		filterArgs := args[:len(args)-cleanupArgTail]
		repoPlaceholderCount := placeholderCountInClause(query, "row.repo_id IN")
		familyPlaceholderCount := placeholderCountInClause(query, "row.family IN")
		for i, arg := range filterArgs {
			value := arg.(string)
			if i >= repoPlaceholderCount && i < repoPlaceholderCount+familyPlaceholderCount {
				families[value] = struct{}{}
				continue
			}
			repoIDs[value] = struct{}{}
		}
	} else {
		scopeID = args[0].(string)
		generationID = args[1].(string)
		includeAmbiguous = args[2].(bool)
		limit = args[3].(int)
		offset = args[4].(int)
	}

	var matches [][]any
	for _, row := range db.rows {
		if latestActive {
			if _, ok := repoIDs[row.RepoID]; !ok {
				continue
			}
			if len(families) > 0 {
				if _, ok := families[row.Family]; !ok {
					continue
				}
			}
		} else {
			if row.ScopeID != scopeID || row.GenerationID != generationID {
				continue
			}
		}
		if !existenceQuery {
			switch row.Reachability {
			case string(IaCReachabilityUnused):
			case string(IaCReachabilityAmbiguous):
				if !includeAmbiguous {
					continue
				}
			default:
				continue
			}
		}
		evidence, _ := json.Marshal(row.Evidence)
		limitations, _ := json.Marshal(row.Limitations)
		matches = append(matches, []any{
			row.ScopeID, row.GenerationID, row.RepoID, row.Family,
			row.ArtifactPath, row.ArtifactName, row.Reachability, row.Finding,
			row.Confidence, evidence, limitations, row.ObservedAt, row.UpdatedAt,
		})
	}
	sort.Slice(matches, func(i, j int) bool {
		leftFamily := matches[i][3].(string)
		rightFamily := matches[j][3].(string)
		if leftFamily != rightFamily {
			return leftFamily < rightFamily
		}
		return matches[i][4].(string) < matches[j][4].(string)
	})
	if countQuery {
		return &iacReachabilityRows{data: [][]any{{len(matches)}}, idx: -1}, nil
	}
	if offset > len(matches) {
		matches = nil
	} else {
		matches = matches[offset:]
	}
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return &iacReachabilityRows{data: matches, idx: -1}, nil
}

func placeholderCountInClause(query string, clause string) int {
	idx := strings.Index(query, clause)
	if idx < 0 {
		return 0
	}
	start := strings.Index(query[idx:], "(")
	end := strings.Index(query[idx:], ")")
	if start < 0 || end < 0 || end <= start {
		return 0
	}
	return strings.Count(query[idx+start:idx+end], "$")
}

type iacReachabilityRows struct {
	data [][]any
	idx  int
}

func (r *iacReachabilityRows) Next() bool {
	r.idx++
	return r.idx < len(r.data)
}

func (r *iacReachabilityRows) Scan(dest ...any) error {
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
			*d = val.(string)
		case *float64:
			*d = val.(float64)
		case *[]byte:
			*d = val.([]byte)
		case *time.Time:
			*d = val.(time.Time)
		case *int:
			*d = val.(int)
		default:
			return fmt.Errorf("unsupported scan dest type %T", dest[i])
		}
	}
	return nil
}

func (r *iacReachabilityRows) Err() error   { return nil }
func (r *iacReachabilityRows) Close() error { return nil }

func iacReachabilityKey(row iacReachabilityStoredRow) string {
	return strings.Join([]string{row.ScopeID, row.GenerationID, row.RepoID, row.Family, row.ArtifactPath}, "|")
}

func decodeStringArrayJSON(raw any) ([]string, error) {
	bytes, ok := raw.([]byte)
	if !ok {
		return nil, fmt.Errorf("json arg type = %T, want []byte", raw)
	}
	var out []string
	if err := json.Unmarshal(bytes, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func iacReachabilityRowsFromExecs(t *testing.T, execs []fakeExecCall) []iacReachabilityStoredRow {
	t.Helper()

	var rows []iacReachabilityStoredRow
	for _, execCall := range execs {
		if !strings.Contains(execCall.query, "INSERT INTO iac_reachability_rows") {
			continue
		}
		const columnsPerRow = 13
		if len(execCall.args)%columnsPerRow != 0 {
			t.Fatalf("iac reachability insert args = %d, want multiple of %d", len(execCall.args), columnsPerRow)
		}
		for i := 0; i < len(execCall.args); i += columnsPerRow {
			evidence, err := decodeStringArrayJSON(execCall.args[i+9])
			if err != nil {
				t.Fatalf("decode evidence: %v", err)
			}
			limitations, err := decodeStringArrayJSON(execCall.args[i+10])
			if err != nil {
				t.Fatalf("decode limitations: %v", err)
			}
			rows = append(rows, iacReachabilityStoredRow{
				ScopeID:      execCall.args[i+0].(string),
				GenerationID: execCall.args[i+1].(string),
				RepoID:       execCall.args[i+2].(string),
				Family:       execCall.args[i+3].(string),
				ArtifactPath: execCall.args[i+4].(string),
				ArtifactName: execCall.args[i+5].(string),
				Reachability: execCall.args[i+6].(string),
				Finding:      execCall.args[i+7].(string),
				Confidence:   execCall.args[i+8].(float64),
				Evidence:     evidence,
				Limitations:  limitations,
				ObservedAt:   execCall.args[i+11].(time.Time),
				UpdatedAt:    execCall.args[i+12].(time.Time),
			})
		}
	}
	return rows
}
