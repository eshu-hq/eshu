// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alerts

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	supplychain "github.com/eshu-hq/eshu/go/internal/query/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type cov7AlertsQueryer struct {
	queries []string
	onRow   func(query string) db.Row
	onRows  func(query string) (db.Rows, error)
}

func (q *cov7AlertsQueryer) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	q.queries = append(q.queries, query)
	return q.onRows(query)
}

func (q *cov7AlertsQueryer) QueryRowContext(_ context.Context, query string, _ ...any) db.Row {
	q.queries = append(q.queries, query)
	return q.onRow(query)
}

func (*cov7AlertsQueryer) BeginReadOnlySnapshot(context.Context) (db.ReadTransaction, error) {
	return nil, errors.New("snapshot unused on the aggregate path")
}

type cov7AlertsRow struct {
	values  []any
	scanErr error
}

func (r *cov7AlertsRow) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	for i, value := range r.values {
		if err := cov7AlertsAssign(dest[i], value); err != nil {
			return err
		}
	}
	return nil
}

func cov7AlertsAssign(target, value any) error {
	switch typed := target.(type) {
	case *string:
		*typed = value.(string)
	case *int64:
		*typed = value.(int64)
	case *sql.NullInt64:
		*typed = value.(sql.NullInt64)
	case *[]byte:
		*typed = value.([]byte)
	default:
		return errors.New("unexpected scan target")
	}
	return nil
}

type cov7AlertsRows struct {
	rows     [][]any
	pos      int
	scanErr  error
	iterErr  error
	closeErr error
}

func (r *cov7AlertsRows) Next() bool { return r.pos < len(r.rows) }

func (r *cov7AlertsRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	row := r.rows[r.pos]
	r.pos++
	for i, value := range row {
		if err := cov7AlertsAssign(dest[i], value); err != nil {
			return err
		}
	}
	return nil
}

func (r *cov7AlertsRows) Err() error { return r.iterErr }

func (r *cov7AlertsRows) Close() error { return r.closeErr }

const cov7AlertPayload = `{
	"provider":"github","provider_alert_id":"1","provider_alert_number":7,
	"provider_state":"open","repository_id":"r1","package_id":"pkg:npm/x",
	"ecosystem":"npm","package_name":"x","manifest_path":"package.json",
	"dependency_scope":"runtime","relationship":"direct",
	"ghsa_ids":["GHSA-a-b-c"],"cve_ids":["CVE-2026-0007"],
	"vulnerable_range":"<2.0","patched_version":"2.0","severity":"high",
	"cvss":{"score":9.8,"empty":null," ":1},
	"epss":{"probability":"0.5","blank":" ","nil":null},
	"cwes":[{"id":"CWE-79","skip":" "},"oops",{}],
	"summary":"s","source_url":"u","created_at":"c","updated_at":"u2",
	"collection_coverage_state":"complete","collection_truncated":true,
	"collection_pages_fetched":3,"collection_state_filter":"open",
	"collection_incomplete_reasons":["r"],
	"reconciliation_status":"matched","reason":"why","reason_code":"rc",
	"impact_wait_outcome":"waited",
	"package_missing_evidence":["current"],
	"missing_evidence":[{"kind":"repo","reason":"no grant","evidence_id":"e1","detail":"d"},{"kind":"","reason":"skip"}],
	"evidence_fact_ids":["f1"],
	"eshu_impact_status":"affected","eshu_impact_finding_id":"finding:1",
	"observed_version":"1.0","requested_range":"^1.0","dependency_range":">=1.0",
	"dependency_evidence_id":"d1","dependency_evidence_kind":"sbom"
}`

func cov7AlertListQueryer(rows db.Rows, err error) *cov7AlertsQueryer {
	return &cov7AlertsQueryer{
		onRows: func(string) (db.Rows, error) { return rows, err },
	}
}

// TestCov7ListReconciliationsDecodesRows proves list guards, failure
// propagation, and the full decode shape behind one reconciliation row.
func TestCov7ListReconciliationsDecodesRows(t *testing.T) {
	t.Parallel()

	if _, err := (PostgresStore{}).ListSecurityAlertReconciliations(t.Context(), supplychain.SecurityAlertReconciliationFilter{RepositoryID: "r", Limit: 1}); err == nil {
		t.Fatalf("nil database list = nil error, want database required")
	}
	reader := cov7AlertListQueryer(&cov7AlertsRows{}, nil)
	guarded := NewPostgresStoreWithReadStore(reader)
	if _, err := guarded.ListSecurityAlertReconciliations(t.Context(), supplychain.SecurityAlertReconciliationFilter{Limit: 1}); err == nil {
		t.Fatalf("anchorless list = nil error, want scope required")
	}
	if _, err := guarded.ListSecurityAlertReconciliations(t.Context(), supplychain.SecurityAlertReconciliationFilter{RepositoryID: "r"}); err == nil {
		t.Fatalf("zero-limit list = nil error, want limit range error")
	}
	if _, err := guarded.ListSecurityAlertReconciliations(t.Context(), supplychain.SecurityAlertReconciliationFilter{RepositoryID: "r", Limit: supplychain.SecurityAlertReconciliationMaxLimit + 2}); err == nil {
		t.Fatalf("oversize list = nil error, want limit range error")
	}
	queryFail := cov7AlertListQueryer(nil, errors.New("list down"))
	if _, err := NewPostgresStoreWithReadStore(queryFail).ListSecurityAlertReconciliations(t.Context(), supplychain.SecurityAlertReconciliationFilter{RepositoryID: "r", Limit: 1}); err == nil {
		t.Fatalf("query failure = nil error, want failure")
	}
	scanFail := cov7AlertListQueryer(&cov7AlertsRows{rows: [][]any{{"f", "s", []byte("{}")}}, scanErr: errors.New("scan boom")}, nil)
	if _, err := NewPostgresStoreWithReadStore(scanFail).ListSecurityAlertReconciliations(t.Context(), supplychain.SecurityAlertReconciliationFilter{RepositoryID: "r", Limit: 1}); err == nil {
		t.Fatalf("scan failure = nil error, want failure")
	}
	decodeFail := cov7AlertListQueryer(&cov7AlertsRows{rows: [][]any{{"f", "s", []byte("{oops}")}}}, nil)
	if _, err := NewPostgresStoreWithReadStore(decodeFail).ListSecurityAlertReconciliations(t.Context(), supplychain.SecurityAlertReconciliationFilter{RepositoryID: "r", Limit: 1}); err == nil {
		t.Fatalf("decode failure = nil error, want failure")
	}
	iterFail := cov7AlertListQueryer(&cov7AlertsRows{
		rows:    [][]any{{"fact:1", "reported", []byte(cov7AlertPayload)}},
		iterErr: errors.New("iter boom"),
	}, nil)
	if _, err := NewPostgresStoreWithReadStore(iterFail).ListSecurityAlertReconciliations(t.Context(), supplychain.SecurityAlertReconciliationFilter{RepositoryID: "r", Limit: 1}); err == nil {
		t.Fatalf("iteration failure = nil error, want failure")
	}
	happy := cov7AlertListQueryer(&cov7AlertsRows{rows: [][]any{{"fact:1", "reported", []byte(cov7AlertPayload)}}}, nil)
	rows, err := NewPostgresStoreWithReadStore(happy).ListSecurityAlertReconciliations(t.Context(), supplychain.SecurityAlertReconciliationFilter{RepositoryID: "r", Limit: 1})
	if err != nil || len(rows) != 1 {
		t.Fatalf("list = (%#v, %v), want one row", rows, err)
	}
	row := rows[0]
	if row.ReconciliationID != "fact:1" || row.ProviderAlert.Provider != "github" || row.ProviderAlert.ProviderAlertNumber != 7 {
		t.Fatalf("provider row = %#v, want shaped alert", row.ProviderAlert)
	}
	if row.ProviderAlert.CVSS["score"] != 9.8 || len(row.ProviderAlert.CVSS) != 1 {
		t.Fatalf("cvss = %#v, want filtered map", row.ProviderAlert.CVSS)
	}
	if row.ProviderAlert.EPSS["probability"] != "0.5" || len(row.ProviderAlert.EPSS) != 1 {
		t.Fatalf("epss = %#v, want stringified map", row.ProviderAlert.EPSS)
	}
	if len(row.ProviderAlert.CWEs) != 1 || row.ProviderAlert.CWEs[0]["id"] != "CWE-79" {
		t.Fatalf("cwes = %#v, want one trimmed row", row.ProviderAlert.CWEs)
	}
	if row.EshuImpact.ImpactStatus != "affected" || row.EshuPackage.ObservedVersion != "1.0" {
		t.Fatalf("impact/package = (%#v, %#v), want decoded rows", row.EshuImpact, row.EshuPackage)
	}
	if len(row.EshuPackage.MissingEvidence) != 1 || row.EshuPackage.MissingEvidence[0] != "current" {
		t.Fatalf("package missing evidence = %#v, want current field winning", row.EshuPackage.MissingEvidence)
	}
	if len(row.MissingEvidence) != 1 || row.MissingEvidence[0].Kind != "repo" || row.MissingEvidence[0].EvidenceID != "e1" {
		t.Fatalf("missing evidence = %#v, want one triage row", row.MissingEvidence)
	}
	if row.SourceFreshness != "active" || row.SourceConfidence != "reported" {
		t.Fatalf("freshness row = (%q, %q), want active/reported", row.SourceFreshness, row.SourceConfidence)
	}
	if row.ProviderAlert.CollectionPagesFetched != 3 || !row.ProviderAlert.CollectionTruncated {
		t.Fatalf("collection = (%d, %v), want 3 pages with truncation",
			row.ProviderAlert.CollectionPagesFetched, row.ProviderAlert.CollectionTruncated)
	}
	if store := NewPostgresStore(nil); store.DB != nil {
		t.Fatalf("store constructor did not keep database")
	}
}

// TestCov7ProviderRepositoryScopes proves the blank-name fast path, failure
// propagation, and trimmed scope reads on both the list and aggregate
// stores.
func TestCov7ProviderRepositoryScopes(t *testing.T) {
	t.Parallel()

	if _, err := (PostgresStore{}).SecurityAlertProviderRepositoryScopes(t.Context(), "repo"); err == nil {
		t.Fatalf("nil list-store scopes = nil error, want database required")
	}
	if _, err := (PostgresAggregateStore{}).SecurityAlertProviderRepositoryScopes(t.Context(), "repo"); err == nil {
		t.Fatalf("nil aggregate scopes = nil error, want database required")
	}
	blank, err := NewPostgresStoreWithReadStore(cov7AlertListQueryer(nil, errors.New("must not query"))).SecurityAlertProviderRepositoryScopes(t.Context(), "  ")
	if err != nil || blank != nil {
		t.Fatalf("blank scopes = (%v, %v), want nil nil without a query", blank, err)
	}
	queryFail := cov7AlertListQueryer(nil, errors.New("scopes down"))
	if _, err := NewPostgresStoreWithReadStore(queryFail).SecurityAlertProviderRepositoryScopes(t.Context(), "repo"); err == nil {
		t.Fatalf("scopes query failure = nil error, want failure")
	}
	scanFail := cov7AlertListQueryer(&cov7AlertsRows{rows: [][]any{{"s1"}}, scanErr: errors.New("scan boom")}, nil)
	if _, err := NewPostgresStoreWithReadStore(scanFail).SecurityAlertProviderRepositoryScopes(t.Context(), "repo"); err == nil {
		t.Fatalf("scopes scan failure = nil error, want failure")
	}
	iterFail := cov7AlertListQueryer(&cov7AlertsRows{rows: [][]any{{"s1"}}, iterErr: errors.New("iter boom")}, nil)
	if _, err := NewPostgresAggregateStoreWithReadStore(iterFail).SecurityAlertProviderRepositoryScopes(t.Context(), "repo"); err == nil {
		t.Fatalf("aggregate scopes iteration failure = nil error, want failure")
	}
	happy := cov7AlertListQueryer(&cov7AlertsRows{rows: [][]any{{" s1 "}, {"s2"}}}, nil)
	scopes, err := NewPostgresAggregateStoreWithReadStore(happy).SecurityAlertProviderRepositoryScopes(t.Context(), "repo")
	if err != nil || len(scopes) != 2 || scopes[0] != "s1" {
		t.Fatalf("scopes = (%v, %v), want trimmed pair", scopes, err)
	}
}

// TestCov7CountReconciliationsReadsBuckets proves the aggregate count
// envelope across the total row and all four bucket dimensions.
func TestCov7CountReconciliationsReadsBuckets(t *testing.T) {
	t.Parallel()

	if _, err := (PostgresAggregateStore{}).CountSecurityAlertReconciliations(t.Context(), supplychain.SecurityAlertReconciliationAggregateFilter{}); err == nil {
		t.Fatalf("nil database count = nil error, want database required")
	}
	scanFail := &cov7AlertsQueryer{
		onRow:  func(string) db.Row { return &cov7AlertsRow{scanErr: errors.New("scan boom")} },
		onRows: func(string) (db.Rows, error) { return &cov7AlertsRows{}, nil },
	}
	if _, err := NewPostgresAggregateStoreWithReadStore(scanFail).CountSecurityAlertReconciliations(t.Context(), supplychain.SecurityAlertReconciliationAggregateFilter{}); err == nil {
		t.Fatalf("total scan failure = nil error, want failure")
	}
	bucketFail := &cov7AlertsQueryer{
		onRow:  func(string) db.Row { return &cov7AlertsRow{values: []any{sql.NullInt64{Int64: 2, Valid: true}}} },
		onRows: func(string) (db.Rows, error) { return nil, errors.New("bucket down") },
	}
	if _, err := NewPostgresAggregateStoreWithReadStore(bucketFail).CountSecurityAlertReconciliations(t.Context(), supplychain.SecurityAlertReconciliationAggregateFilter{}); err == nil {
		t.Fatalf("bucket query failure = nil error, want failure")
	}
	bucketScanFail := &cov7AlertsQueryer{
		onRow: func(string) db.Row { return &cov7AlertsRow{values: []any{sql.NullInt64{Int64: 2, Valid: true}}} },
		onRows: func(string) (db.Rows, error) {
			return &cov7AlertsRows{rows: [][]any{{"a", int64(1)}}, scanErr: errors.New("scan boom")}, nil
		},
	}
	if _, err := NewPostgresAggregateStoreWithReadStore(bucketScanFail).CountSecurityAlertReconciliations(t.Context(), supplychain.SecurityAlertReconciliationAggregateFilter{}); err == nil {
		t.Fatalf("bucket scan failure = nil error, want failure")
	}
	bucketIterFail := &cov7AlertsQueryer{
		onRow: func(string) db.Row { return &cov7AlertsRow{values: []any{sql.NullInt64{Int64: 2, Valid: true}}} },
		onRows: func(string) (db.Rows, error) {
			return &cov7AlertsRows{rows: [][]any{{"a", int64(1)}}, iterErr: errors.New("iter boom")}, nil
		},
	}
	if _, err := NewPostgresAggregateStoreWithReadStore(bucketIterFail).CountSecurityAlertReconciliations(t.Context(), supplychain.SecurityAlertReconciliationAggregateFilter{}); err == nil {
		t.Fatalf("bucket iteration failure = nil error, want failure")
	}
	bucketFills := [][][]any{
		{{"matched", int64(2)}},
		{{"github", int64(2)}},
		{{"open", int64(1)}, {"fixed", int64(1)}},
		{{"active", int64(2)}},
	}
	calls := 0
	happy := &cov7AlertsQueryer{
		onRow: func(string) db.Row { return &cov7AlertsRow{values: []any{sql.NullInt64{Int64: 2, Valid: true}}} },
		onRows: func(string) (db.Rows, error) {
			rows := bucketFills[calls%len(bucketFills)]
			calls++
			return &cov7AlertsRows{rows: rows}, nil
		},
	}
	count, err := NewPostgresAggregateStoreWithReadStore(happy).CountSecurityAlertReconciliations(t.Context(), supplychain.SecurityAlertReconciliationAggregateFilter{})
	if err != nil {
		t.Fatalf("count = %v, want envelope", err)
	}
	if count.TotalReconciliations != 2 || calls != 4 {
		t.Fatalf("count = (%#v, fills %d), want total 2 with 4 bucket fills", count, calls)
	}
	if count.ByReconciliationStatus["matched"] != 2 || count.ByProvider["github"] != 2 ||
		count.ByProviderState["open"] != 1 || count.BySourceFreshness["active"] != 2 {
		t.Fatalf("buckets = %#v, want filled maps", count)
	}
	if store := NewPostgresAggregateStore(nil); store.DB != nil {
		t.Fatalf("aggregate constructor did not keep database")
	}
}

// TestCov7ReconciliationInventoryValidation proves dimension, limit, and
// failure handling plus a trimmed happy row.
func TestCov7ReconciliationInventoryValidation(t *testing.T) {
	t.Parallel()

	store := NewPostgresAggregateStoreWithReadStore(cov7AlertListQueryer(&cov7AlertsRows{}, nil))
	filter := supplychain.SecurityAlertReconciliationAggregateFilter{}
	if _, err := store.SecurityAlertReconciliationInventory(t.Context(), filter, "bogus", 10, 0); err == nil {
		t.Fatalf("bogus dimension = nil error, want unsupported dimension")
	}
	if _, err := store.SecurityAlertReconciliationInventory(t.Context(), filter, supplychain.SecurityAlertReconciliationInventoryByStatus, 0, 0); err == nil {
		t.Fatalf("zero limit = nil error, want limit range error")
	}
	if _, err := store.SecurityAlertReconciliationInventory(t.Context(), filter, supplychain.SecurityAlertReconciliationInventoryByStatus, supplychain.SecurityAlertReconciliationAggregateMaxLimit+2, 0); err == nil {
		t.Fatalf("oversize limit = nil error, want limit range error")
	}
	if _, err := (PostgresAggregateStore{}).SecurityAlertReconciliationInventory(t.Context(), filter, supplychain.SecurityAlertReconciliationInventoryByStatus, 10, 0); err == nil {
		t.Fatalf("nil database inventory = nil error, want database required")
	}
	queryFail := NewPostgresAggregateStoreWithReadStore(cov7AlertListQueryer(nil, errors.New("inventory down")))
	if _, err := queryFail.SecurityAlertReconciliationInventory(t.Context(), filter, supplychain.SecurityAlertReconciliationInventoryByStatus, 10, 0); err == nil {
		t.Fatalf("inventory query failure = nil error, want failure")
	}
	scanFail := NewPostgresAggregateStoreWithReadStore(cov7AlertListQueryer(&cov7AlertsRows{rows: [][]any{{"x", int64(1)}}, scanErr: errors.New("scan boom")}, nil))
	if _, err := scanFail.SecurityAlertReconciliationInventory(t.Context(), filter, supplychain.SecurityAlertReconciliationInventoryByStatus, 10, 0); err == nil {
		t.Fatalf("inventory scan failure = nil error, want failure")
	}
	iterFail := NewPostgresAggregateStoreWithReadStore(cov7AlertListQueryer(&cov7AlertsRows{rows: [][]any{{"x", int64(1)}}, iterErr: errors.New("iter boom")}, nil))
	if _, err := iterFail.SecurityAlertReconciliationInventory(t.Context(), filter, supplychain.SecurityAlertReconciliationInventoryByStatus, 10, 0); err == nil {
		t.Fatalf("inventory iteration failure = nil error, want failure")
	}
	happy := NewPostgresAggregateStoreWithReadStore(cov7AlertListQueryer(&cov7AlertsRows{rows: [][]any{{" matched ", int64(3)}}}, nil))
	rows, err := happy.SecurityAlertReconciliationInventory(t.Context(), filter, supplychain.SecurityAlertReconciliationInventoryByProvider, 10, -4)
	if err != nil || len(rows) != 1 || rows[0].Value != "matched" || rows[0].Count != 3 || rows[0].Dimension != supplychain.SecurityAlertReconciliationInventoryByProvider {
		t.Fatalf("inventory = (%#v, %v), want trimmed provider row", rows, err)
	}
	exprs := map[string]bool{}
	for _, dimension := range []supplychain.SecurityAlertReconciliationInventoryDimension{
		supplychain.SecurityAlertReconciliationInventoryByStatus,
		supplychain.SecurityAlertReconciliationInventoryByProvider,
		supplychain.SecurityAlertReconciliationInventoryByProviderState,
		supplychain.SecurityAlertReconciliationInventoryByRepository,
		supplychain.SecurityAlertReconciliationInventoryByPackage,
	} {
		expr, err := inventoryGroupExpression(dimension)
		if err != nil || expr == "" || exprs[expr] {
			t.Fatalf("dimension %q = (%q, %v), want distinct expression", dimension, expr, err)
		}
		exprs[expr] = true
	}
}

// TestCov7AlertValueHelpers proves the payload accessors and freshness
// derivations behind decoded rows.
func TestCov7AlertValueHelpers(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"m": map[string]any{"a": 1, " ": 2, "n": nil},
		"s": map[string]any{"k": " v ", "blank": "  ", "nil": nil},
		"l": []any{map[string]any{"k": "v", "drop": nil}, "skip", map[string]any{}},
	}
	if got := mapVal(payload, "m"); len(got) != 1 {
		t.Fatalf("mapVal = %#v, want filtered map", got)
	}
	if mapVal(payload, "missing") != nil || mapVal(payload, "s") == nil {
		t.Fatalf("mapVal presence wrong")
	}
	if got := StringMapVal(payload, "s"); len(got) != 1 || got["k"] != "v" {
		t.Fatalf("StringMapVal = %#v, want stringified singleton", got)
	}
	if StringMapVal(payload, "missing") != nil {
		t.Fatalf("StringMapVal missing non-nil")
	}
	if got := stringMapSliceVal(payload, "l"); len(got) != 1 || got[0]["k"] != "v" {
		t.Fatalf("stringMapSlice = %#v, want one trimmed row", got)
	}
	if stringMapSliceVal(payload, "missing") != nil {
		t.Fatalf("stringMapSlice missing non-nil")
	}
	if got := sourceFreshness(map[string]any{"source_freshness": "stale"}); got != "stale" {
		t.Fatalf("explicit freshness = %q, want passthrough", got)
	}
	if got := sourceFreshness(map[string]any{"collection_coverage_state": "incomplete"}); got != "partial" {
		t.Fatalf("incomplete freshness = %q, want partial", got)
	}
	if got := sourceFreshness(map[string]any{}); got != "active" {
		t.Fatalf("default freshness = %q, want active", got)
	}
	if got := packageMissingEvidence([]string{"c"}, []string{"l"}); len(got) != 1 || got[0] != "c" {
		t.Fatalf("current evidence = %#v, want current winning", got)
	}
	if got := packageMissingEvidence(nil, []string{"l"}); len(got) != 1 || got[0] != "l" {
		t.Fatalf("legacy evidence = %#v, want legacy fallback", got)
	}
	triage := missingEvidenceVal(map[string]any{"k": []any{
		map[string]any{"kind": "repo", "reason": "why", "evidence_id": nil, "detail": nil},
		map[string]any{"kind": "", "reason": "skip"},
		"skip",
	}}, "k")
	if len(triage) != 1 || triage[0].Kind != "repo" || triage[0].EvidenceID != "" || triage[0].Detail != "" {
		t.Fatalf("triage = %#v, want one normalized row", triage)
	}
	if missingEvidenceVal(map[string]any{}, "k") != nil {
		t.Fatalf("absent triage non-nil")
	}
}
