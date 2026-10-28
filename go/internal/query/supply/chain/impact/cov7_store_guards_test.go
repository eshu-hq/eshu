// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type cov7FailingDriver struct{}

func (cov7FailingDriver) Open(string) (driver.Conn, error) { return nil, errors.New("no direct open") }

type cov7FailingConnector struct{ err error }

func (c cov7FailingConnector) Connect(context.Context) (driver.Conn, error) {
	return nil, c.err
}

func (cov7FailingConnector) Driver() driver.Driver { return cov7FailingDriver{} }

type cov7StoreRow struct {
	values  []any
	scanErr error
}

func (r *cov7StoreRow) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	for i, value := range r.values {
		if err := cov7AssignScanTarget(dest[i], value); err != nil {
			return err
		}
	}
	return nil
}

func cov7AssignScanTarget(target, value any) error {
	switch typed := target.(type) {
	case *string:
		*typed = value.(string)
	case *bool:
		*typed = value.(bool)
	case *int64:
		*typed = value.(int64)
	case *sql.NullInt64:
		*typed = value.(sql.NullInt64)
	case *[]byte:
		*typed = value.([]byte)
	case *time.Time:
		*typed = value.(time.Time)
	default:
		return errors.New("unexpected scan target")
	}
	return nil
}

type cov7StoreRows struct {
	rows     [][]any
	pos      int
	scanErr  error
	iterErr  error
	closeErr error
}

func (r *cov7StoreRows) Next() bool { return r.pos < len(r.rows) }

func (r *cov7StoreRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	row := r.rows[r.pos]
	r.pos++
	for i, value := range row {
		if err := cov7AssignScanTarget(dest[i], value); err != nil {
			return err
		}
	}
	return nil
}

func (r *cov7StoreRows) Err() error { return r.iterErr }

func (r *cov7StoreRows) Close() error { return r.closeErr }

type cov7StoreQueryer struct {
	queries []string
	onRow   func(query string) db.Row
	onRows  func(query string) (db.Rows, error)
}

func (q *cov7StoreQueryer) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	q.queries = append(q.queries, query)
	return q.onRows(query)
}

func (q *cov7StoreQueryer) QueryRowContext(_ context.Context, query string, _ ...any) db.Row {
	q.queries = append(q.queries, query)
	return q.onRow(query)
}

func (*cov7StoreQueryer) BeginReadOnlySnapshot(context.Context) (db.ReadTransaction, error) {
	return nil, errors.New("snapshot unused on the aggregate path")
}

func cov7NullInt(v int64) sql.NullInt64 { return sql.NullInt64{Int64: v, Valid: true} }

// TestCov7WinnersReadGateAndClock proves the env-gate bool semantics and the
// suppression clock defaulting to UTC now.
func TestCov7WinnersReadGateAndClock(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"true", "1", "t", "TRUE", "True"} {
		if !WinnersReadEnabled(value) {
			t.Fatalf("WinnersReadEnabled(%q) = false, want true", value)
		}
	}
	for _, value := range []string{"", "false", "0", "bogus", "yes"} {
		if WinnersReadEnabled(value) {
			t.Fatalf("WinnersReadEnabled(%q) = true, want false", value)
		}
	}
	if got := SuppressionReadAt(nil); got.IsZero() || got.Location() != time.UTC {
		t.Fatalf("nil clock read-at = %v, want non-zero UTC", got)
	}
	fixed := time.Date(2026, 5, 1, 12, 0, 0, 0, time.FixedZone("x", 3600))
	if got := SuppressionReadAt(func() time.Time { return fixed }); !got.Equal(fixed) || got.Location() != time.UTC {
		t.Fatalf("fixed clock read-at = %v, want UTC normalized", got)
	}
	if filter := (FindingFilter{CVEID: "c"}); !filter.HasScope() {
		t.Fatalf("cve filter lacks scope, want true")
	}
	if filter := (FindingFilter{}); filter.HasScope() {
		t.Fatalf("empty filter has scope, want false")
	}
}

// TestCov7AggregateCountReadsBuckets proves the count envelope: the nil
// database guard, scan propagation, bucket-query failures, and a happy path
// with priority and severity buckets.
func TestCov7AggregateCountReadsBuckets(t *testing.T) {
	t.Parallel()

	if _, err := (PostgresAggregateStore{}).CountSupplyChainImpactFindings(t.Context(), AggregateFilter{}); err == nil {
		t.Fatalf("nil database count = nil error, want database required")
	}
	// The legacy *sql.DB path runs through legacyAggregateQueryer: a refused
	// connection fails the count-row scan there instead of at the reader.
	legacy := NewPostgresAggregateStore(sql.OpenDB(cov7FailingConnector{err: errors.New("connect refused")}))
	if _, err := legacy.CountSupplyChainImpactFindings(t.Context(), AggregateFilter{}); err == nil {
		t.Fatalf("legacy count failure = nil error, want failure")
	}
	scanFail := &cov7StoreQueryer{
		onRow:  func(string) db.Row { return &cov7StoreRow{scanErr: errors.New("scan boom")} },
		onRows: func(string) (db.Rows, error) { return &cov7StoreRows{}, nil },
	}
	if _, err := NewPostgresAggregateStoreWithReadStore(scanFail).CountSupplyChainImpactFindings(t.Context(), AggregateFilter{}); err == nil {
		t.Fatalf("reader count scan failure = nil error, want failure")
	}
	bucketFail := &cov7StoreQueryer{
		onRow: func(string) db.Row {
			return &cov7StoreRow{values: []any{cov7NullInt(3), cov7NullInt(2), cov7NullInt(1), cov7NullInt(1), cov7NullInt(0), cov7NullInt(1)}}
		},
		onRows: func(string) (db.Rows, error) { return nil, errors.New("bucket down") },
	}
	if _, err := NewPostgresAggregateStoreWithReadStore(bucketFail).CountSupplyChainImpactFindings(t.Context(), AggregateFilter{}); err == nil {
		t.Fatalf("bucket query failure = nil error, want failure")
	}
	bucketScanFail := &cov7StoreQueryer{
		onRow: func(string) db.Row {
			return &cov7StoreRow{values: []any{cov7NullInt(3), cov7NullInt(2), cov7NullInt(1), cov7NullInt(1), cov7NullInt(0), cov7NullInt(1)}}
		},
		onRows: func(string) (db.Rows, error) {
			return &cov7StoreRows{rows: [][]any{{"p1", int64(2)}}, scanErr: errors.New("scan boom")}, nil
		},
	}
	if _, err := NewPostgresAggregateStoreWithReadStore(bucketScanFail).CountSupplyChainImpactFindings(t.Context(), AggregateFilter{}); err == nil {
		t.Fatalf("bucket scan failure = nil error, want failure")
	}
	calls := 0
	happy := &cov7StoreQueryer{
		onRow: func(string) db.Row {
			return &cov7StoreRow{values: []any{cov7NullInt(3), cov7NullInt(2), cov7NullInt(1), cov7NullInt(1), cov7NullInt(0), cov7NullInt(1)}}
		},
		onRows: func(string) (db.Rows, error) {
			calls++
			if calls == 1 {
				return &cov7StoreRows{rows: [][]any{{"p1", int64(2)}, {"p2", int64(1)}}}, nil
			}
			return &cov7StoreRows{rows: [][]any{{"critical", int64(3)}}}, nil
		},
	}
	count, err := NewPostgresAggregateStoreWithReadStore(happy).CountSupplyChainImpactFindings(t.Context(), AggregateFilter{})
	if err != nil {
		t.Fatalf("count = %v, want envelope", err)
	}
	if count.TotalFindings != 3 || count.AffectedFindings != 2 || count.AffectedExact != 1 ||
		count.AffectedDerived != 1 || count.PossiblyAffected != 0 || count.NotAffected != 1 {
		t.Fatalf("count = %#v, want scanned totals", count)
	}
	if count.ByPriorityBucket["p1"] != 2 || count.BySeverity["critical"] != 3 {
		t.Fatalf("buckets = (%v, %v), want filled maps", count.ByPriorityBucket, count.BySeverity)
	}
	if store := NewPostgresAggregateStore(nil); store.DB != nil {
		t.Fatalf("aggregate constructor did not keep database")
	}
}

// TestCov7AggregateInventoryValidation proves dimension, limit, and database
// validation plus a happy grouped page with trimmed values.
func TestCov7AggregateInventoryValidation(t *testing.T) {
	t.Parallel()

	store := NewPostgresAggregateStoreWithReadStore(&cov7StoreQueryer{
		onRows: func(string) (db.Rows, error) { return &cov7StoreRows{}, nil },
	})
	if _, err := store.SupplyChainImpactInventory(t.Context(), AggregateFilter{}, "bogus", 10, 0); err == nil {
		t.Fatalf("bogus dimension = nil error, want unsupported dimension")
	}
	if _, err := store.SupplyChainImpactInventory(t.Context(), AggregateFilter{}, InventoryByStatus, 0, 0); err == nil {
		t.Fatalf("zero limit = nil error, want limit range error")
	}
	if _, err := store.SupplyChainImpactInventory(t.Context(), AggregateFilter{}, InventoryByStatus, AggregateMaxLimit+2, 0); err == nil {
		t.Fatalf("oversize limit = nil error, want limit range error")
	}
	if _, err := (PostgresAggregateStore{}).SupplyChainImpactInventory(t.Context(), AggregateFilter{}, InventoryByStatus, 10, 0); err == nil {
		t.Fatalf("nil database inventory = nil error, want database required")
	}
	queryFail := NewPostgresAggregateStoreWithReadStore(&cov7StoreQueryer{
		onRows: func(string) (db.Rows, error) { return nil, errors.New("inventory down") },
	})
	if _, err := queryFail.SupplyChainImpactInventory(t.Context(), AggregateFilter{}, InventoryByStatus, 10, 0); err == nil {
		t.Fatalf("inventory query failure = nil error, want failure")
	}
	scanFail := NewPostgresAggregateStoreWithReadStore(&cov7StoreQueryer{
		onRows: func(string) (db.Rows, error) {
			return &cov7StoreRows{rows: [][]any{{"x", int64(1)}}, scanErr: errors.New("scan boom")}, nil
		},
	})
	if _, err := scanFail.SupplyChainImpactInventory(t.Context(), AggregateFilter{}, InventoryByStatus, 10, 0); err == nil {
		t.Fatalf("inventory scan failure = nil error, want failure")
	}
	iterFail := NewPostgresAggregateStoreWithReadStore(&cov7StoreQueryer{
		onRows: func(string) (db.Rows, error) {
			return &cov7StoreRows{rows: [][]any{{"x", int64(1)}}, iterErr: errors.New("iter boom")}, nil
		},
	})
	if _, err := iterFail.SupplyChainImpactInventory(t.Context(), AggregateFilter{}, InventoryByStatus, 10, 0); err == nil {
		t.Fatalf("inventory iteration failure = nil error, want failure")
	}
	happy := NewPostgresAggregateStoreWithReadStore(&cov7StoreQueryer{
		onRows: func(string) (db.Rows, error) {
			return &cov7StoreRows{rows: [][]any{{" affected_exact ", int64(4)}}}, nil
		},
	})
	rows, err := happy.SupplyChainImpactInventory(t.Context(), AggregateFilter{}, InventoryBySeverity, 10, -3)
	if err != nil || len(rows) != 1 || rows[0].Value != "affected_exact" || rows[0].Count != 4 || rows[0].Dimension != InventoryBySeverity {
		t.Fatalf("inventory = (%#v, %v), want trimmed severity row", rows, err)
	}
	exprs := map[string]bool{}
	for _, dimension := range []InventoryDimension{InventoryByStatus, InventoryByPriorityBucket, InventoryBySeverity, InventoryByRepository, InventoryByEcosystem} {
		expr, err := supplyChainImpactInventoryGroupExpression(dimension)
		if err != nil || expr == "" || exprs[expr] {
			t.Fatalf("dimension %q = (%q, %v), want distinct expression", dimension, expr, err)
		}
		exprs[expr] = true
	}
	rendered := InventoryQuery("GROUP_EXPR")
	if strings.Count(rendered, "GROUP_EXPR") != 1 {
		t.Fatalf("inventory query did not substitute the group expression once")
	}
}

// TestCov7WinnersWatermarkProbe proves the legacy no-op, the nil-database
// guard, and the watermark row mapping.
func TestCov7WinnersWatermarkProbe(t *testing.T) {
	t.Parallel()

	legacy := NewPostgresFindingStoreWithReadStore(&cov7StoreQueryer{
		onRows: func(string) (db.Rows, error) { return nil, errors.New("must not query") },
	}, false)
	freshness, err := legacy.SupplyChainImpactWinnersWatermark(t.Context())
	if err != nil || freshness.ServingFromWinners {
		t.Fatalf("legacy watermark = (%#v, %v), want no-op", freshness, err)
	}
	gated := PostgresFindingStore{ReadFromWinners: true}
	freshness, err = gated.SupplyChainImpactWinnersWatermark(t.Context())
	if err == nil || !freshness.ServingFromWinners {
		t.Fatalf("nil database watermark = (%#v, %v), want guarded error", freshness, err)
	}
	queryFail := NewPostgresFindingStoreWithReadStore(&cov7StoreQueryer{
		onRows: func(string) (db.Rows, error) { return nil, errors.New("watermark down") },
	}, true)
	if _, err := queryFail.SupplyChainImpactWinnersWatermark(t.Context()); err == nil {
		t.Fatalf("watermark query failure = nil error, want failure")
	}
	building := NewPostgresFindingStoreWithReadStore(&cov7StoreQueryer{
		onRows: func(string) (db.Rows, error) { return &cov7StoreRows{}, nil },
	}, true)
	freshness, err = building.SupplyChainImpactWinnersWatermark(t.Context())
	if err != nil || !freshness.ServingFromWinners || freshness.Present {
		t.Fatalf("building watermark = (%#v, %v), want absent present=false", freshness, err)
	}
	scanFail := NewPostgresFindingStoreWithReadStore(&cov7StoreQueryer{
		onRows: func(string) (db.Rows, error) {
			return &cov7StoreRows{rows: [][]any{{"x"}}, scanErr: errors.New("scan boom")}, nil
		},
	}, true)
	if _, err := scanFail.SupplyChainImpactWinnersWatermark(t.Context()); err == nil {
		t.Fatalf("watermark scan failure = nil error, want failure")
	}
	stamp := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	happy := NewPostgresFindingStoreWithReadStore(&cov7StoreQueryer{
		onRows: func(string) (db.Rows, error) {
			return &cov7StoreRows{rows: [][]any{{stamp}}}, nil
		},
	}, true)
	freshness, err = happy.SupplyChainImpactWinnersWatermark(t.Context())
	if err != nil || !freshness.Present || !freshness.MaterializedAt.Equal(stamp) {
		t.Fatalf("watermark = (%#v, %v), want present stamp", freshness, err)
	}
}

// TestCov7ListFindingsValidation proves list guards, failure propagation, a
// decoded happy page, and the winners-query selection rule.
func TestCov7ListFindingsValidation(t *testing.T) {
	t.Parallel()

	if _, err := (PostgresFindingStore{}).ListSupplyChainImpactFindings(t.Context(), FindingFilter{CVEID: "c", Limit: 1}); err == nil {
		t.Fatalf("nil database list = nil error, want database required")
	}
	guarded := NewPostgresFindingStoreWithReadStore(&cov7StoreQueryer{
		onRows: func(string) (db.Rows, error) { return &cov7StoreRows{}, nil },
	}, false)
	if _, err := guarded.ListSupplyChainImpactFindings(t.Context(), FindingFilter{Limit: 1}); err == nil {
		t.Fatalf("anchorless list = nil error, want scope required")
	}
	if _, err := guarded.ListSupplyChainImpactFindings(t.Context(), FindingFilter{CVEID: "c"}); err == nil {
		t.Fatalf("zero-limit list = nil error, want limit range error")
	}
	queryFail := NewPostgresFindingStoreWithReadStore(&cov7StoreQueryer{
		onRows: func(string) (db.Rows, error) { return nil, errors.New("list down") },
	}, false)
	if _, err := queryFail.ListSupplyChainImpactFindings(t.Context(), FindingFilter{CVEID: "c", Limit: 1}); err == nil {
		t.Fatalf("list query failure = nil error, want failure")
	}
	scanFail := NewPostgresFindingStoreWithReadStore(&cov7StoreQueryer{
		onRows: func(string) (db.Rows, error) {
			return &cov7StoreRows{rows: [][]any{{"f", "s", []byte("{}")}}, scanErr: errors.New("scan boom")}, nil
		},
	}, false)
	if _, err := scanFail.ListSupplyChainImpactFindings(t.Context(), FindingFilter{CVEID: "c", Limit: 1}); err == nil {
		t.Fatalf("list scan failure = nil error, want failure")
	}
	decodeFail := NewPostgresFindingStoreWithReadStore(&cov7StoreQueryer{
		onRows: func(string) (db.Rows, error) {
			return &cov7StoreRows{rows: [][]any{{"f", "s", []byte("{oops")}}, scanErr: nil}, nil
		},
	}, false)
	if _, err := decodeFail.ListSupplyChainImpactFindings(t.Context(), FindingFilter{CVEID: "c", Limit: 1}); err == nil {
		t.Fatalf("list decode failure = nil error, want failure")
	}
	iterFail := NewPostgresFindingStoreWithReadStore(&cov7StoreQueryer{
		onRows: func(string) (db.Rows, error) {
			return &cov7StoreRows{
				rows:    [][]any{{"fact:abc", "source", []byte(`{"finding_id":"finding:abc","evidence_fact_ids":["fact:evidence"]}`)}},
				iterErr: errors.New("iter boom"),
			}, nil
		},
	}, false)
	if _, err := iterFail.ListSupplyChainImpactFindings(t.Context(), FindingFilter{CVEID: "c", Limit: 1}); err == nil {
		t.Fatalf("list iteration failure = nil error, want failure")
	}
	happyQueryer := &cov7StoreQueryer{
		onRows: func(string) (db.Rows, error) {
			return &cov7StoreRows{rows: [][]any{{"fact:abc", "source", []byte(`{"finding_id":"finding:abc","evidence_fact_ids":["fact:evidence"]}`)}}}, nil
		},
	}
	happy := NewPostgresFindingStoreWithReadStore(happyQueryer, false)
	rows, err := happy.ListSupplyChainImpactFindings(t.Context(), FindingFilter{CVEID: "c", Limit: 1})
	if err != nil || len(rows) != 1 || rows[0].FindingID != "fact:abc" {
		t.Fatalf("list = (%#v, %v), want decoded row", rows, err)
	}
	winnersQueryer := &cov7StoreQueryer{
		onRows: func(string) (db.Rows, error) { return &cov7StoreRows{}, nil },
	}
	winners := NewPostgresFindingStoreWithReadStore(winnersQueryer, true)
	if _, err := winners.ListSupplyChainImpactFindings(t.Context(), FindingFilter{CVEID: "c", Limit: 1}); err != nil {
		t.Fatalf("winners list = %v, want rows", err)
	}
	scopedQueryer := &cov7StoreQueryer{
		onRows: func(string) (db.Rows, error) { return &cov7StoreRows{}, nil },
	}
	scoped := NewPostgresFindingStoreWithReadStore(scopedQueryer, true)
	if _, err := scoped.ListSupplyChainImpactFindings(t.Context(), FindingFilter{CVEID: "c", Limit: 1, AllowedRepositoryIDs: []string{"r1"}}); err != nil {
		t.Fatalf("scoped winners list = %v, want rows", err)
	}
	if winnersQueryer.queries[0] == scopedQueryer.queries[0] {
		t.Fatalf("winners and scoped reads issued the same query, want distinct statements")
	}
	if store := NewPostgresFindingStore(nil); store.DB != nil {
		t.Fatalf("finding constructor did not keep database")
	}
	if store := NewPostgresFindingStoreWithReadModel(nil, true); !store.ReadFromWinners {
		t.Fatalf("read-model constructor dropped the winners gate")
	}
}

// TestCov7AmbiguousExplanationError proves the candidate-count floor, the
// errors.Is match, and the count extraction across error shapes.
func TestCov7AmbiguousExplanationError(t *testing.T) {
	t.Parallel()

	floored := newSupplyChainImpactExplanationAmbiguousError(0)
	if got := ExplanationAmbiguousCandidateCount(floored); got != 2 {
		t.Fatalf("floored count = %d, want 2", got)
	}
	if floored.Error() != ErrExplanationAmbiguous.Error() {
		t.Fatalf("error text = %q, want ambiguous message", floored.Error())
	}
	if !errors.Is(floored, ErrExplanationAmbiguous) {
		t.Fatalf("errors.Is = false, want ambiguous match")
	}
	many := newSupplyChainImpactExplanationAmbiguousError(5)
	if got := ExplanationAmbiguousCandidateCount(many); got != 5 {
		t.Fatalf("count = %d, want 5", got)
	}
	if got := ExplanationAmbiguousCandidateCount(ErrExplanationAmbiguous); got != 2 {
		t.Fatalf("sentinel count = %d, want 2", got)
	}
	if got := ExplanationAmbiguousCandidateCount(errors.New("other")); got != 0 {
		t.Fatalf("foreign count = %d, want 0", got)
	}
	if got := ExplanationAmbiguousCandidateCount(nil); got != 0 {
		t.Fatalf("nil count = %d, want 0", got)
	}
}
