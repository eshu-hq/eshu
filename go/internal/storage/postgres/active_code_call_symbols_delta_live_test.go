// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10003 row 1 disposable PostgreSQL proof that LoadActiveCodeCallSymbolDefinitionFacts overlays a delta-active git scope on its #7797 base full generation: the unchanged bar definition of the base stays visible to the code-call materialization pass; reads ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE

import (
	"bytes"
	"context"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

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

// seedCallSymbolGoMod seeds a go.mod manifest for a delta-fixture scope so
// the Go producer read anchors keys on that scope. The tag generation must
// already carry activated_at, so call it after activating the full
// generation: the manifest read resolves content only for an activated tag.
func seedCallSymbolGoMod(f *deltaManifestFixture, scopeID, module, generationID string) {
	f.t.Helper()
	f.exec("seed go.mod "+scopeID, `
INSERT INTO content_files
    (repo_id, relative_path, content, content_hash, line_count, language, indexed_at, generation_id)
VALUES ($1, 'go.mod', $2, md5($2), 3, 'gomod', clock_timestamp(), $3)`,
		scopeID, "module "+module+"\n\ngo 1.24\n", generationID)
}

// TestLoadActiveCodeCallSymbolDefinitionFactsDeltaAnchoredBaseBindingLive is
// the #10140 anchored-leg proof for the row-1 delta overlay. A delta-active
// scope whose only anchored keys are scip-go gomod keys keeps its unchanged
// base definition visible through the anchored base scan, with the producer
// scopes bound at $5 and the candidate base generations at $6. The active
// legs return only the delta row, so a dropped or swapped base-leg binding
// loses the base definition and this test fails.
func TestLoadActiveCodeCallSymbolDefinitionFactsDeltaAnchoredBaseBindingLive(t *testing.T) {
	const (
		scopeID  = "call-anchor-scope"
		fullID   = "call-anchor-full"
		deltaID  = "call-anchor-delta"
		module   = "github.com/acme/anchored"
		deltaKey = "scip-go gomod github.com/acme/anchored Client#Delta()."
		baseKey  = "scip-go gomod github.com/acme/anchored Server#Serve()."
	)
	f := openDeltaManifestFixture(t)
	f.scope(scopeID)
	f.generation(scopeID, deltaManifestGeneration{id: fullID, commit: "c0"})
	f.activate(scopeID, fullID)
	seedCallSymbolGoMod(f, scopeID, module, fullID)
	seedCallSymbolFile(f.t, f, scopeID, fullID, "anchor-full-delta", "file:"+scopeID+":src/delta.go", "src/delta.go", deltaKey)
	seedCallSymbolFile(f.t, f, scopeID, fullID, "anchor-full-base", "file:"+scopeID+":src/base.go", "src/base.go", baseKey)
	f.generation(scopeID, deltaManifestGeneration{
		id: deltaID, isDelta: true, commit: "c1", baseline: "c0", unionBase: "c0",
	})
	f.deltaRepository(scopeID, deltaID, []string{"src/delta.go"}, nil)
	seedCallSymbolFile(f.t, f, scopeID, deltaID, "anchor-delta",
		"file:"+scopeID+":src/delta.go", "src/delta.go", deltaKey)
	f.activate(scopeID, deltaID)

	queryer := &recordingCodeCallSymbolQueryer{SQLDB: SQLDB{DB: f.database}}
	loaded, err := NewFactStore(queryer).LoadActiveCodeCallSymbolDefinitionFacts(f.ctx, []string{deltaKey, baseKey})
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts(): %v", err)
	}
	got := factIDs(loaded)
	slices.Sort(got)
	if want := []string{"anchor-delta", "anchor-full-base"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded fact ids = %#v, want %#v (base definition lost without the anchored base scan)", got, want)
	}

	// The anchored base text is derived from the shipped constant, never
	// hand-copied, so the statement match cannot drift false-green.
	anchoredBase, err := deriveDeltaOverlayBaseQuery(
		listAnchoredActiveCodeCallSymbolDefinitionFactsQuery, codeCallAnchoredSymbolDeltaOverlayScopeParam)
	if err != nil {
		t.Fatalf("derive anchored base query: %v", err)
	}
	var baseArgs []any
	for i, query := range queryer.queries {
		if query == anchoredBase {
			baseArgs = queryer.args[i]
		}
	}
	if baseArgs == nil {
		t.Fatalf("anchored base scan did not run: %d statements, none matches the derived anchored base text", len(queryer.queries))
	}
	if got, want := baseArgs[4], []string{scopeID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("anchored base producer scopes ($5) = %#v, want %#v", got, want)
	}
	baseGens, ok := baseArgs[5].([]string)
	if !ok || !slices.Contains(baseGens, fullID) {
		t.Fatalf("anchored base candidate generations ($6) = %#v, want it to contain %q", baseArgs[5], fullID)
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

// rewriteContentManifest retags one stored manifest with a delta generation's
// rewritten content, the way the content writer records a delta that changed
// the manifest itself.
func rewriteContentManifest(f *deltaManifestFixture, scopeID, generationID, relativePath, content string) {
	f.t.Helper()
	f.exec("rewrite manifest "+scopeID+"/"+relativePath, `
UPDATE content_files SET content = $1, content_hash = md5($1), generation_id = $2
WHERE repo_id = $3 AND relative_path = $4`,
		content, generationID, scopeID, relativePath)
}

// seedAnchoredUnionScopes seeds one package-anchored and one Go-anchored
// scope, each with a full generation holding a producer manifest and one
// definition, plus a delta generation on top. When rewriteManifests is true
// the deltas rename the manifests, so active producer discovery comes back
// empty while the base generations still hold the definitions (#10104 firing
// shape); otherwise the deltas touch an unrelated file and discovery still
// anchors (manifest-stable shape). It returns the two anchored keys to load.
func seedAnchoredUnionScopes(t *testing.T, f *deltaManifestFixture, prefix string, rewriteManifests bool) []string {
	t.Helper()
	now := time.Now().UTC()
	packageScope, goScope := prefix+"-pkg-scope", prefix+"-go-scope"
	packageFull, goFull := prefix+"-pkg-full", prefix+"-go-full"
	packageDelta, goDelta := prefix+"-pkg-delta", prefix+"-go-delta"

	f.scope(packageScope)
	f.generation(packageScope, deltaManifestGeneration{id: packageFull, commit: "c0"})
	seedActiveCodeCallSymbolManifest(t, f.ctx, f.database, packageScope, "package.json", `{"name":"alpha-pkg"}`, packageFull, now)
	seedActiveCodeCallSymbolPackageFact(t, f.ctx, f.database, prefix+"-pkg-base", packageScope, packageFull, "src/widget.js", "alpha-pkg", "Widget", now)
	f.activate(packageScope, packageFull)
	f.generation(packageScope, deltaManifestGeneration{id: packageDelta, isDelta: true, commit: "c1", baseline: "c0", unionBase: "c0"})
	if rewriteManifests {
		rewriteContentManifest(f, packageScope, packageDelta, "package.json", `{"name":"renamed-pkg"}`)
		f.deltaRepository(packageScope, packageDelta, []string{"package.json"}, nil)
	} else {
		f.deltaRepository(packageScope, packageDelta, []string{"src/other.js"}, nil)
	}
	f.activate(packageScope, packageDelta)

	f.scope(goScope)
	f.generation(goScope, deltaManifestGeneration{id: goFull, commit: "c0"})
	seedActiveCodeCallSymbolGoMod(t, f.ctx, f.database, goScope, "go.mod", "github.com/acme/widget", goFull, now)
	seedActiveCodeCallSymbolFact(t, f.ctx, f.database, prefix+"-go-base", goScope, goFull, "lib/widget.go", "scip-go gomod github.com/acme/widget New().", now)
	f.activate(goScope, goFull)
	f.generation(goScope, deltaManifestGeneration{id: goDelta, isDelta: true, commit: "c1", baseline: "c0", unionBase: "c0"})
	if rewriteManifests {
		rewriteContentManifest(f, goScope, goDelta, "go.mod", "module github.com/acme/renamed\n\ngo 1.24\n")
		f.deltaRepository(goScope, goDelta, []string{"go.mod"}, nil)
	} else {
		f.deltaRepository(goScope, goDelta, []string{"lib/other.js"}, nil)
	}
	f.activate(goScope, goDelta)

	return []string{"package:alpha-pkg#Widget", "scip-go gomod github.com/acme/widget New()."}
}

// TestLoadActiveCodeCallSymbolDefinitionFactsManifestChangingDeltaLive is the
// #10104 regression: both deltas rename their producer manifests, so active
// producer discovery comes back empty for the package and Go groups while the
// base generations still hold the definitions. Pre-fix the anchored legs
// vanish with their base scans and the load returns nothing; post-fix each
// group runs a base-only union leg ($5 baselined-target scopes, $6 candidate
// base generations) and both base definitions load, adding only the union
// base pages to the tally.
func TestLoadActiveCodeCallSymbolDefinitionFactsManifestChangingDeltaLive(t *testing.T) {
	f := openDeltaManifestFixture(t)
	keys := seedAnchoredUnionScopes(t, f, "call-union", true)

	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	got := loadCallSymbolIDs(t, f, keys)
	t.Logf("loaded call symbol facts for manifest-changing delta scopes = %v", got)
	want := []string{"call-union-go-base", "call-union-pkg-base"}
	if !slices.Equal(got, want) {
		t.Fatalf("loaded call symbol facts = %v, want %v", got, want)
	}
	assertDeltaManifestOutcomes(t,
		f.deltaUnionOutcomesForMetric(t, "eshu_dp_code_call_symbol_definition_read_delta_union_total"),
		map[string]int64{"union_base_found": 2})
	for _, want := range []string{
		"package_producer_scope_count=0",
		"go_producer_scope_count=0",
		"package_union_scope_count=2",
		"go_union_scope_count=2",
	} {
		if !strings.Contains(logged.String(), want) {
			t.Fatalf("load log is missing %q:\n%s", want, logged.String())
		}
	}

	counter := &overlayCountingQueryer{SQLDB: SQLDB{DB: f.database}}
	store := NewFactStore(counter)
	loaded, err := store.LoadActiveCodeCallSymbolDefinitionFacts(f.ctx, keys)
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts(): %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("loaded %d facts, want 2 union base definitions", len(loaded))
	}
	tally := tallyCallSymbolStatements(t, counter.queries)
	t.Logf("manifest-changing statements: active=%d targets=%d base=%d resolve=%d inputs=%d other=%d",
		tally.active, tally.targets, tally.base, tally.resolve, tally.inputs, tally.other)
	if tally.active != 0 || tally.targets != 1 || tally.base != 2 || tally.resolve != 2 || tally.inputs != 1 || tally.other != 2 {
		t.Fatalf("statement tally = %+v, want active=0 targets=1 base=2 resolve=2 inputs=1 other=2", tally)
	}
}

// TestLoadActiveCodeCallSymbolDefinitionFactsManifestStableAnchoredLive pins
// the anchored manifest-stable shape the row-1 PR covers by construction:
// the deltas leave both manifests alone, discovery anchors both groups, and
// the unchanged base definitions stay visible through the same leg+base
// machinery the union legs reuse. It passes identically pre- and post-#10104.
func TestLoadActiveCodeCallSymbolDefinitionFactsManifestStableAnchoredLive(t *testing.T) {
	f := openDeltaManifestFixture(t)
	keys := seedAnchoredUnionScopes(t, f, "call-stable", false)

	got := loadCallSymbolIDs(t, f, keys)
	t.Logf("loaded call symbol facts for manifest-stable delta scopes = %v", got)
	want := []string{"call-stable-go-base", "call-stable-pkg-base"}
	if !slices.Equal(got, want) {
		t.Fatalf("loaded call symbol facts = %v, want %v", got, want)
	}
	assertDeltaManifestOutcomes(t,
		f.deltaUnionOutcomesForMetric(t, "eshu_dp_code_call_symbol_definition_read_delta_union_total"),
		map[string]int64{"union_base_found": 2})

	counter := &overlayCountingQueryer{SQLDB: SQLDB{DB: f.database}}
	store := NewFactStore(counter)
	loaded, err := store.LoadActiveCodeCallSymbolDefinitionFacts(f.ctx, keys)
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts(): %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("loaded %d facts, want 2 overlaid base definitions", len(loaded))
	}
	tally := tallyCallSymbolStatements(t, counter.queries)
	t.Logf("manifest-stable statements: active=%d targets=%d base=%d resolve=%d inputs=%d other=%d",
		tally.active, tally.targets, tally.base, tally.resolve, tally.inputs, tally.other)
	if tally.active != 2 || tally.targets != 1 || tally.base != 2 || tally.resolve != 2 || tally.inputs != 1 || tally.other != 2 {
		t.Fatalf("statement tally = %+v, want active=2 targets=1 base=2 resolve=2 inputs=1 other=2", tally)
	}
}

// TestLoadActiveCodeCallSymbolDefinitionFactsAnchoredNoDeltaStatementsLive
// proves an anchored read with no delta-active scope issues no base, resolve,
// or inputs statement: only the producer reads, the anchored active scans,
// and the single delta-target lookup run. It passes identically pre- and
// post-#10104, pinning the no-delta steady state for anchored keys.
func TestLoadActiveCodeCallSymbolDefinitionFactsAnchoredNoDeltaStatementsLive(t *testing.T) {
	f := openDeltaManifestFixture(t)
	now := time.Now().UTC()
	f.scope("call-anchored-pkg-scope")
	f.generation("call-anchored-pkg-scope", deltaManifestGeneration{id: "call-anchored-pkg-full", commit: "c0"})
	seedActiveCodeCallSymbolManifest(t, f.ctx, f.database, "call-anchored-pkg-scope", "package.json", `{"name":"alpha-pkg"}`, "call-anchored-pkg-full", now)
	seedActiveCodeCallSymbolPackageFact(t, f.ctx, f.database, "anchored-pkg-def", "call-anchored-pkg-scope", "call-anchored-pkg-full", "src/widget.js", "alpha-pkg", "Widget", now)
	f.activate("call-anchored-pkg-scope", "call-anchored-pkg-full")
	f.scope("call-anchored-go-scope")
	f.generation("call-anchored-go-scope", deltaManifestGeneration{id: "call-anchored-go-full", commit: "c0"})
	seedActiveCodeCallSymbolGoMod(t, f.ctx, f.database, "call-anchored-go-scope", "go.mod", "github.com/acme/widget", "call-anchored-go-full", now)
	seedActiveCodeCallSymbolFact(t, f.ctx, f.database, "anchored-go-def", "call-anchored-go-scope", "call-anchored-go-full", "lib/widget.go", "scip-go gomod github.com/acme/widget New().", now)
	f.activate("call-anchored-go-scope", "call-anchored-go-full")
	keys := []string{"package:alpha-pkg#Widget", "scip-go gomod github.com/acme/widget New()."}

	counter := &overlayCountingQueryer{SQLDB: SQLDB{DB: f.database}}
	store := NewFactStore(counter)
	loaded, err := store.LoadActiveCodeCallSymbolDefinitionFacts(f.ctx, keys)
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts(): %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("loaded %d facts, want 2 anchored definitions", len(loaded))
	}
	tally := tallyCallSymbolStatements(t, counter.queries)
	t.Logf("anchored no-delta statements: active=%d targets=%d base=%d resolve=%d inputs=%d other=%d",
		tally.active, tally.targets, tally.base, tally.resolve, tally.inputs, tally.other)
	if tally.active != 2 || tally.targets != 1 || tally.base != 0 || tally.resolve != 0 || tally.inputs != 0 || tally.other != 2 {
		t.Fatalf("statement tally = %+v, want active=2 targets=1 base=0 resolve=0 inputs=0 other=0+2 discovery", tally)
	}
}
