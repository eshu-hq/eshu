// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10003 row 1 disposable PostgreSQL proof that LoadActiveCodeCallSymbolDefinitionFacts overlays a delta-active git scope on its #7797 base full generation: the unchanged bar definition of the base stays visible to the code-call materialization pass; reads ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// seedCallSymbolFile seeds one file fact carrying a single scip_symbol
// definition. key is the stable fact key the delta overlay matches on.
func seedCallSymbolFile(t *testing.T, f *deltaManifestFixture, scopeID, generationID, factID, key, relativePath, symbol string) {
	t.Helper()
	f.exec("seed call symbol fact "+factID, `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
     source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, 'file', $4,
        'git', $4, clock_timestamp(), clock_timestamp(), FALSE,
        jsonb_build_object(
            'repo_id', $2::text,
            'relative_path', $5::text,
            'parsed_file_data', jsonb_build_object(
                'functions', jsonb_build_array(jsonb_build_object('uid', 'uid:' || $1, 'scip_symbol', $6::text))
            )
        ))`,
		factID, scopeID, generationID, key, relativePath, symbol)
}

// loadCallSymbolIDs runs the production loader and returns the sorted loaded
// fact ids.
func loadCallSymbolIDs(t *testing.T, f *deltaManifestFixture, keys []string) []string {
	t.Helper()
	loaded, err := f.store.LoadActiveCodeCallSymbolDefinitionFacts(f.ctx, keys)
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts(): %v", err)
	}
	ids := make([]string, 0, len(loaded))
	for _, envelope := range loaded {
		ids = append(ids, envelope.FactID)
	}
	slices.Sort(ids)
	return ids
}

// seedCallSymbolBase seeds scopeID with a full generation at commit c0
// holding foo (src/foo.go) and bar (src/bar.go) definitions, activated.
func seedCallSymbolBase(f *deltaManifestFixture, scopeID, fullID string) {
	f.t.Helper()
	f.scope(scopeID)
	f.generation(scopeID, deltaManifestGeneration{id: fullID, commit: "c0"})
	seedCallSymbolFile(f.t, f, scopeID, fullID, "full-foo", "file:"+scopeID+":src/foo.go", "src/foo.go", "scip-foo-symbol")
	seedCallSymbolFile(f.t, f, scopeID, fullID, "full-bar", "file:"+scopeID+":src/bar.go", "src/bar.go", "scip-bar-symbol")
	f.activate(scopeID, fullID)
}

// overlayCountingQueryer counts every statement a delta-overlay read issues,
// classifying each by which overlay phase it belongs to. The snapshot
// transaction is wrapped the same way so statements inside the
// repeatable-read snapshot are counted too.
type overlayCountingQueryer struct {
	SQLDB
	mu      sync.Mutex
	queries []string
}

func (q *overlayCountingQueryer) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	q.mu.Lock()
	q.queries = append(q.queries, query)
	q.mu.Unlock()
	return q.SQLDB.QueryContext(ctx, query, args...)
}

func (q *overlayCountingQueryer) BeginReadOnlyRepeatableRead(ctx context.Context) (db.Transaction, error) {
	tx, err := q.SQLDB.BeginReadOnlyRepeatableRead(ctx)
	if err != nil {
		return nil, err
	}
	return &overlayCountingTransaction{Transaction: tx, counter: q}, nil
}

type overlayCountingTransaction struct {
	db.Transaction
	counter *overlayCountingQueryer
}

func (t *overlayCountingTransaction) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	t.counter.mu.Lock()
	t.counter.queries = append(t.counter.queries, query)
	t.counter.mu.Unlock()
	return t.Transaction.QueryContext(ctx, query, args...)
}

// overlayStatementTally classifies the counted statements of one read. resolve
// counts the delta-base resolution statements resolveDeltaBases issues (the
// batched rule: two statements no matter how many scopes it resolves).
type overlayStatementTally struct {
	active  int
	targets int
	base    int
	resolve int
	inputs  int
	other   int
}

func tallyCallSymbolStatements(t *testing.T, queries []string) overlayStatementTally {
	t.Helper()
	var tally overlayStatementTally
	for _, query := range queries {
		switch {
		case query == deltaOverlayTargetsQuery:
			tally.targets++
		case query == deltaOverlayInputsQuery:
			tally.inputs++
		case query == listActiveCodeCallSymbolDefinitionFactsQuery ||
			query == listAnchoredActiveCodeCallSymbolDefinitionFactsQuery:
			tally.active++
		case strings.Contains(query, "active_delta"):
			tally.base++
		case strings.Contains(query, "unnest($1::text[], $2::text[])"):
			tally.resolve++
		default:
			tally.other++
			t.Logf("unclassified statement: %.120q", query)
		}
	}
	return tally
}

// TestLoadActiveCodeCallSymbolDefinitionFactsDeltaActiveScopeLive is the
// #10003 row 1 regression. A full generation holds two definitions (foo in
// src/foo.go, bar in src/bar.go); a delta generation changes only src/foo.go,
// re-emits foo under the same stable key, and becomes the scope's active
// generation. The code-call materialization pass resolves callee symbols
// through this loader, so the unchanged bar definition must stay visible.
// Before the fix the read joined only the active generation and returned
// [delta-foo], so the pass retracted the still-correct main->bar edge and
// never re-emitted it.
func TestLoadActiveCodeCallSymbolDefinitionFactsDeltaActiveScopeLive(t *testing.T) {
	f := openDeltaManifestFixture(t)
	seedCallSymbolBase(f, "call-delta-scope", "call-full-generation")
	f.generation("call-delta-scope", deltaManifestGeneration{
		id: "call-live-delta", isDelta: true, commit: "c1", baseline: "c0", unionBase: "c0",
	})
	f.deltaRepository("call-delta-scope", "call-live-delta", []string{"src/foo.go"}, nil)
	seedCallSymbolFile(f.t, f, "call-delta-scope", "call-live-delta", "delta-foo",
		"file:call-delta-scope:src/foo.go", "src/foo.go", "scip-foo-symbol")
	f.activate("call-delta-scope", "call-live-delta")

	got := loadCallSymbolIDs(t, f, []string{"scip-foo-symbol", "scip-bar-symbol"})
	t.Logf("loaded call symbol facts for delta-active scope = %v", got)
	want := []string{"delta-foo", "full-bar"}
	if !slices.Equal(got, want) {
		t.Fatalf("loaded call symbol facts = %v, want %v", got, want)
	}
	assertDeltaManifestOutcomes(t,
		f.deltaUnionOutcomesForMetric(t, "eshu_dp_code_call_symbol_definition_read_delta_union_total"),
		map[string]int64{"union_base_found": 1})
}

// deltaUnionOutcomesForMetric returns one delta-union decision counter by
// outcome for the named metric (#10003 rows 1-2 arbiter ruling (b)): the
// overlay path must move a real counter, not only span events and logs.
func (f *deltaManifestFixture) deltaUnionOutcomesForMetric(t *testing.T, metricName string) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := f.reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	outcomes := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != metricName {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s is %T, want an int64 sum", m.Name, m.Data)
			}
			for _, dp := range sum.DataPoints {
				outcome, _ := dp.Attributes.Value(telemetry.MetricDimensionOutcome)
				outcomes[outcome.AsString()] += dp.Value
			}
		}
	}
	return outcomes
}

// TestLoadActiveCodeCallSymbolDefinitionFactsDeltaStatementsLive proves the
// fixed read issues the overlay statements on a delta-active scope: the
// active scan, one delta-target lookup, the derived base scan, and the
// overlay inputs read.
func TestLoadActiveCodeCallSymbolDefinitionFactsDeltaStatementsLive(t *testing.T) {
	f := openDeltaManifestFixture(t)
	seedCallSymbolBase(f, "call-stmt-scope", "call-stmt-full")
	f.generation("call-stmt-scope", deltaManifestGeneration{
		id: "call-stmt-delta", isDelta: true, commit: "c1", baseline: "c0", unionBase: "c0",
	})
	f.deltaRepository("call-stmt-scope", "call-stmt-delta", []string{"src/foo.go"}, nil)
	seedCallSymbolFile(f.t, f, "call-stmt-scope", "call-stmt-delta", "delta-stmt-foo",
		"file:call-stmt-scope:src/foo.go", "src/foo.go", "scip-foo-symbol")
	f.activate("call-stmt-scope", "call-stmt-delta")

	counter := &overlayCountingQueryer{SQLDB: SQLDB{DB: f.database}}
	store := NewFactStore(counter)
	loaded, err := store.LoadActiveCodeCallSymbolDefinitionFacts(f.ctx, []string{"scip-foo-symbol", "scip-bar-symbol"})
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts(): %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("loaded %d facts, want 2 (delta foo plus overlaid base bar)", len(loaded))
	}
	tally := tallyCallSymbolStatements(t, counter.queries)
	t.Logf("delta-active statements: active=%d targets=%d base=%d resolve=%d inputs=%d other=%d",
		tally.active, tally.targets, tally.base, tally.resolve, tally.inputs, tally.other)
	if tally.active != 1 || tally.targets != 1 || tally.base != 1 || tally.resolve != 2 || tally.inputs != 1 || tally.other != 0 {
		t.Fatalf("statement tally = %+v, want active=1 targets=1 base=1 resolve=2 inputs=1 other=0", tally)
	}
}

// TestLoadActiveCodeCallSymbolDefinitionFactsNoDeltaStatementsLive proves a
// read with no delta-active scope gains no base or inputs statement: only
// the active scan and the single delta-target lookup run.
func TestLoadActiveCodeCallSymbolDefinitionFactsNoDeltaStatementsLive(t *testing.T) {
	f := openDeltaManifestFixture(t)
	seedCallSymbolBase(f, "call-nodelta-scope", "call-nodelta-full")

	counter := &overlayCountingQueryer{SQLDB: SQLDB{DB: f.database}}
	store := NewFactStore(counter)
	loaded, err := store.LoadActiveCodeCallSymbolDefinitionFacts(f.ctx, []string{"scip-foo-symbol", "scip-bar-symbol"})
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts(): %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("loaded %d facts, want 2", len(loaded))
	}
	tally := tallyCallSymbolStatements(t, counter.queries)
	t.Logf("no-delta statements: active=%d targets=%d base=%d resolve=%d inputs=%d other=%d",
		tally.active, tally.targets, tally.base, tally.resolve, tally.inputs, tally.other)
	if tally.active != 1 || tally.targets != 1 || tally.base != 0 || tally.resolve != 0 || tally.inputs != 0 || tally.other != 0 {
		t.Fatalf("statement tally = %+v, want active=1 targets=1 base=0 resolve=0 inputs=0 other=0", tally)
	}
}

// TestDeriveDeltaOverlayBaseQueryForCodeCallReaders is the hermetic shape
// proof for both definition scans: the unanchored corpus-wide text binds the
// candidate base generation ids at $5 and the anchored producer text at $6,
// and a text that already binds the placeholder fails closed.
func TestDeriveDeltaOverlayBaseQueryForCodeCallReaders(t *testing.T) {
	unanchored, err := deriveDeltaOverlayBaseQuery(listActiveCodeCallSymbolDefinitionFactsQuery, 5)
	if err != nil {
		t.Fatalf("derive unanchored base query: %v", err)
	}
	if !strings.Contains(unanchored, "active_delta") {
		t.Fatalf("derived unanchored base query carries no active_delta join")
	}
	anchored, err := deriveDeltaOverlayBaseQuery(listAnchoredActiveCodeCallSymbolDefinitionFactsQuery, 6)
	if err != nil {
		t.Fatalf("derive anchored base query: %v", err)
	}
	if !strings.Contains(anchored, "active_delta") {
		t.Fatalf("derived anchored base query carries no active_delta join")
	}
	if _, err := deriveDeltaOverlayBaseQuery(listActiveCodeCallSymbolDefinitionFactsQuery, 1); err == nil {
		t.Fatalf("derive with $1 bound by the active text succeeded, want fail-closed")
	}
}
