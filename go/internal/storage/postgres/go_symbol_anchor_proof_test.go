// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	producerstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/code/producers"
)

// sortedFactIDs returns the fact ids of the envelopes in sorted order, so two
// loads compare as sets.
func sortedFactIDs(envelopes []facts.Envelope) []string {
	ids := factIDs(envelopes)
	sort.Strings(ids)
	return ids
}

// TestReducerContentionGateActiveCodeCallSymbolGoAnchorEqualsCorpusScan proves
// on real Postgres that anchoring scip-go gomod keys on stored go.mod modules
// (#7623) loads exactly what the production corpus-wide statement loads for the
// layouts the Go parser produces: a module root, a library used from another
// repository, a fork declaring the same module, a nested module in its own
// go.mod, a /v2 module and a v2/ subdirectory of the root module, a copy under
// third_party with its own go.mod, a superseded generation, a scope that is not
// active, and lookalike modules that share only a string prefix (one longer and
// one shorter than a requested module). The oracle
// is the shipped corpus-wide constant, not a copy.
func TestReducerContentionGateActiveCodeCallSymbolGoAnchorEqualsCorpusScan(t *testing.T) {
	ctx, database := openActiveCodeCallSymbolContentSchema(t)
	now := time.Now().UTC()

	scope := func(scopeID, repoID string) {
		seedActiveCodeCallSymbolRepositoryScope(t, ctx, database, scopeID, repoID, "generation-"+scopeID, now)
	}
	fact := func(factID, scopeID, path, key string, offset int) {
		seedActiveCodeCallSymbolFact(t, ctx, database, factID, scopeID, "generation-"+scopeID, path, key, now.Add(time.Duration(offset)*time.Millisecond))
	}

	scope("scope:lib", "repository:r_lib")
	seedActiveCodeCallSymbolGoMod(t, ctx, database, "repository:r_lib", "go.mod", "github.com/acme/lib", now)
	fact("fact-lib-root", "scope:lib", "lib.go", "scip-go gomod github.com/acme/lib New().", 1)
	fact("fact-lib-client", "scope:lib", "client/client.go", "scip-go gomod github.com/acme/lib/client Client#Request().", 2)
	// A v2/ subdirectory of the root module: its import path continues the root
	// module path, so the root module's scope is the producer.
	fact("fact-lib-v2-dir", "scope:lib", "v2/x/x.go", "scip-go gomod github.com/acme/lib/v2/x Dir().", 3)
	// The superseded generation of the same scope never loads.
	seedActiveCodeCallSymbolFact(t, ctx, database, "fact-lib-stale", "scope:lib", "generation-scope:lib-stale", "old.go", "scip-go gomod github.com/acme/lib/client Client#Request().", now.Add(-time.Second))

	// A caller in another repository: its own module, no definition of the keys.
	scope("scope:app", "repository:r_app")
	seedActiveCodeCallSymbolGoMod(t, ctx, database, "repository:r_app", "go.mod", "github.com/acme/app", now)
	fact("fact-app-main", "scope:app", "main.go", "scip-go gomod github.com/acme/app Run().", 4)

	// A /v2 module declared by its own repository.
	scope("scope:lib-v2", "repository:r_lib_v2")
	seedActiveCodeCallSymbolGoMod(t, ctx, database, "repository:r_lib_v2", "go.mod", "github.com/acme/lib/v2", now)
	fact("fact-lib-v2", "scope:lib-v2", "y/y.go", "scip-go gomod github.com/acme/lib/v2/y Mod().", 5)

	// A multi-module repository: a root module and a nested one in sub/go.mod.
	scope("scope:mono", "repository:r_mono")
	seedActiveCodeCallSymbolGoMod(t, ctx, database, "repository:r_mono", "go.mod", "github.com/acme/mono", now)
	seedActiveCodeCallSymbolGoMod(t, ctx, database, "repository:r_mono", "sub/go.mod", "github.com/acme/mono/sub", now)
	fact("fact-mono-tools", "scope:mono", "tools/run.go", "scip-go gomod github.com/acme/mono/tools Run().", 6)
	fact("fact-mono-sub", "scope:mono", "sub/api/api.go", "scip-go gomod github.com/acme/mono/sub/api Serve().", 7)

	// One module declared by two repositories: both load, so the reducer keeps
	// the key unresolved.
	scope("scope:fork-a", "repository:r_fork_a")
	seedActiveCodeCallSymbolGoMod(t, ctx, database, "repository:r_fork_a", "go.mod", "github.com/acme/forked", now)
	fact("fact-fork-a", "scope:fork-a", "f.go", "scip-go gomod github.com/acme/forked Thing().", 8)
	scope("scope:fork-b", "repository:r_fork_b")
	seedActiveCodeCallSymbolGoMod(t, ctx, database, "repository:r_fork_b", "go.mod", "github.com/acme/forked", now)
	fact("fact-fork-b", "scope:fork-b", "f.go", "scip-go gomod github.com/acme/forked Thing().", 9)

	// A copy kept under third_party with its own go.mod: discovery does not
	// prune that directory, so it is a producer like any other module.
	scope("scope:third", "repository:r_third")
	seedActiveCodeCallSymbolGoMod(t, ctx, database, "repository:r_third", "third_party/lib/go.mod", "github.com/acme/thirdcopy", now)
	fact("fact-third", "scope:third", "third_party/lib/t.go", "scip-go gomod github.com/acme/thirdcopy Copy().", 10)

	// A lookalike module that shares only a string prefix with github.com/acme/lib.
	scope("scope:ext", "repository:r_ext")
	seedActiveCodeCallSymbolGoMod(t, ctx, database, "repository:r_ext", "go.mod", "github.com/acme/libext", now)
	fact("fact-ext", "scope:ext", "e.go", "scip-go gomod github.com/acme/libext Other().", 11)

	// A module whose path is only a string prefix of a requested import path
	// (github.com/acme/mon against github.com/acme/mono/tools): with no path
	// boundary it would be pulled into $5.
	scope("scope:short", "repository:r_short")
	seedActiveCodeCallSymbolGoMod(t, ctx, database, "repository:r_short", "go.mod", "github.com/acme/mon", now)
	fact("fact-short", "scope:short", "s.go", "scip-go gomod github.com/acme/mon Short().", 13)

	// A scope whose go.mod is stored but whose active generation is no longer
	// active: it must stay out of the producer scopes passed as $5.
	scope("scope:frozen", "repository:r_frozen")
	seedActiveCodeCallSymbolGoMod(t, ctx, database, "repository:r_frozen", "go.mod", "github.com/acme/frozen", now)
	fact("fact-frozen", "scope:frozen", "z.go", "scip-go gomod github.com/acme/frozen Old().", 12)
	if _, err := database.ExecContext(ctx, `UPDATE scope_generations SET status = 'superseded' WHERE generation_id = 'generation-scope:frozen'`); err != nil {
		t.Fatalf("supersede frozen generation: %v", err)
	}

	keys := []string{
		"scip-go gomod github.com/acme/lib New().",
		"scip-go gomod github.com/acme/lib/client Client#Request().",
		"scip-go gomod github.com/acme/lib/v2/x Dir().",
		"scip-go gomod github.com/acme/lib/v2/y Mod().",
		"scip-go gomod github.com/acme/mono/tools Run().",
		"scip-go gomod github.com/acme/mono/sub/api Serve().",
		"scip-go gomod github.com/acme/forked Thing().",
		"scip-go gomod github.com/acme/thirdcopy Copy().",
		"scip-go gomod github.com/acme/frozen Old().",
		"scip-go gomod context Background().",
		"scip-go gomod github.com/acme/nobody Missing().",
	}

	store := NewFactStore(SQLDB{DB: database})
	corpus, err := store.loadActiveCodeCallSymbolDefinitionFacts(ctx, listActiveCodeCallSymbolDefinitionFactsQuery, keys, nil)
	if err != nil {
		t.Fatalf("corpus-wide scan error = %v, want nil", err)
	}
	queryer := &recordingCodeCallSymbolQueryer{SQLDB: SQLDB{DB: database}}
	anchored, err := NewFactStore(queryer).LoadActiveCodeCallSymbolDefinitionFacts(ctx, keys)
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}

	want := []string{
		"fact-fork-a", "fact-fork-b", "fact-lib-client", "fact-lib-root", "fact-lib-v2",
		"fact-lib-v2-dir", "fact-mono-sub", "fact-mono-tools", "fact-third",
	}
	if got := sortedFactIDs(corpus); !reflect.DeepEqual(got, want) {
		t.Fatalf("fixture drifted from the corpus-wide oracle:\ncorpus = %v\nwant   = %v", got, want)
	}
	if got := sortedFactIDs(anchored); !reflect.DeepEqual(got, want) {
		t.Fatalf("anchored load = %v, want exactly the corpus-wide result %v", got, want)
	}
	if got, want := queryer.queries, []string{producerstore.GoModuleManifestsQuery, listAnchoredActiveCodeCallSymbolDefinitionFactsQuery}; !reflect.DeepEqual(got, want) {
		t.Fatalf("issued %d statements, want the go.mod read then one anchored scan (no corpus-wide scan)", len(got))
	}
	wantScopes := []string{"scope:fork-a", "scope:fork-b", "scope:lib", "scope:lib-v2", "scope:mono", "scope:third"}
	if scopes, _ := queryer.args[1][4].([]string); !reflect.DeepEqual(scopes, wantScopes) {
		t.Fatalf("anchored producer scopes ($5) = %v, want %v (lookalike, caller-only and inactive scopes are out)", scopes, wantScopes)
	}
}

// TestReducerContentionGateActiveCodeCallSymbolGoAnchorStdlibKeysIssueNoDefinitionScan
// proves a request whose Go keys no stored go.mod declares (the standard
// library) runs the go.mod read and never a definition scan. The corpus-wide
// scan would still match a definition stored under such a key; the anchor does
// not look for one, because the parser only stamps a key from a go.mod it read.
func TestReducerContentionGateActiveCodeCallSymbolGoAnchorStdlibKeysIssueNoDefinitionScan(t *testing.T) {
	ctx, database := openActiveCodeCallSymbolContentSchema(t)
	now := time.Now().UTC()
	seedActiveCodeCallSymbolRepositoryScope(t, ctx, database, "scope:lib", "repository:r_lib", "generation-lib", now)
	seedActiveCodeCallSymbolGoMod(t, ctx, database, "repository:r_lib", "go.mod", "github.com/acme/lib", now)
	seedActiveCodeCallSymbolFact(t, ctx, database, "fact-lib", "scope:lib", "generation-lib", "lib.go", "scip-go gomod github.com/acme/lib New().", now)
	// A definition stored under a standard library key, in a scope with no
	// go.mod: only the corpus-wide scan could find it.
	seedActiveCodeCallSymbolRepositoryScope(t, ctx, database, "scope:odd", "repository:r_odd", "generation-odd", now)
	seedActiveCodeCallSymbolFact(t, ctx, database, "fact-odd", "scope:odd", "generation-odd", "odd.go", "scip-go gomod fmt Printf().", now)
	keys := []string{"scip-go gomod context Background().", "scip-go gomod fmt Printf()."}

	queryer := &recordingCodeCallSymbolQueryer{SQLDB: SQLDB{DB: database}}
	loaded, err := NewFactStore(queryer).LoadActiveCodeCallSymbolDefinitionFacts(ctx, keys)
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("loaded = %v, want none", sortedFactIDs(loaded))
	}
	if got, want := queryer.queries, []string{producerstore.GoModuleManifestsQuery}; !reflect.DeepEqual(got, want) {
		t.Fatalf("issued %d statements, want only the go.mod read", len(got))
	}
	corpus, err := NewFactStore(SQLDB{DB: database}).loadActiveCodeCallSymbolDefinitionFacts(ctx, listActiveCodeCallSymbolDefinitionFactsQuery, keys, nil)
	if err != nil {
		t.Fatalf("corpus-wide scan error = %v, want nil", err)
	}
	if got := sortedFactIDs(corpus); !reflect.DeepEqual(got, []string{"fact-odd"}) {
		t.Fatalf("corpus-wide scan = %v, want the odd definition: the fixture no longer pins the narrowing", got)
	}
}

// TestReducerContentionGateActiveCodeCallSymbolGoAnchorPagesPast500Rows proves
// an anchored Go load pages past the 500-row page size, each page re-running
// only the producer-sized scan, and returns every row once.
func TestReducerContentionGateActiveCodeCallSymbolGoAnchorPagesPast500Rows(t *testing.T) {
	ctx, database := openActiveCodeCallSymbolContentSchema(t)
	now := time.Now().UTC()
	seedActiveCodeCallSymbolRepositoryScope(t, ctx, database, "scope:pages", "repository:r_pages", "generation-pages", now)
	seedActiveCodeCallSymbolGoMod(t, ctx, database, "repository:r_pages", "go.mod", "github.com/acme/pages", now)
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, payload
)
SELECT 'fact-pages-' || g,
    'scope:pages', 'generation-pages', 'file',
    'file:scope:pages:page' || g || '.go',
    'git', 'page' || g || '.go',
    $1::timestamptz + (g || ' seconds')::interval, $1::timestamptz + (g || ' seconds')::interval,
    jsonb_build_object(
        'repo_id', 'scope:pages',
        'relative_path', 'page' || g || '.go',
        'parsed_file_data', jsonb_build_object(
            'functions', jsonb_build_array(jsonb_build_object('uid', 'uid:page:' || g, 'scip_symbol', $2::text))
        )
    )
FROM generate_series(1, 1200) AS g`, now, "scip-go gomod github.com/acme/pages/p Do()."); err != nil {
		t.Fatalf("seed page facts: %v", err)
	}

	queryer := &recordingCodeCallSymbolQueryer{SQLDB: SQLDB{DB: database}}
	loaded, err := NewFactStore(queryer).LoadActiveCodeCallSymbolDefinitionFacts(ctx, []string{"scip-go gomod github.com/acme/pages/p Do()."})
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}
	if got, want := len(loaded), 1200; got != want {
		t.Fatalf("loaded %d facts, want %d", got, want)
	}
	if got, want := len(sortedFactIDs(loaded)), 1200; got != want || len(uniqueStrings(sortedFactIDs(loaded))) != want {
		t.Fatalf("loaded ids are not 1200 distinct values")
	}
	want := []string{
		producerstore.GoModuleManifestsQuery,
		listAnchoredActiveCodeCallSymbolDefinitionFactsQuery,
		listAnchoredActiveCodeCallSymbolDefinitionFactsQuery,
		listAnchoredActiveCodeCallSymbolDefinitionFactsQuery,
	}
	if !reflect.DeepEqual(queryer.queries, want) {
		t.Fatalf("issued %d statements, want the go.mod read plus one anchored scan per 500-row page", len(queryer.queries))
	}
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

// TestReducerContentionGateActiveCodeCallSymbolThreeLegPartitionDedupes proves
// a request with a package key, a Go key and a key no manifest anchors runs
// three separate scans, one per kind, and returns a fact that matches two of
// them once.
func TestReducerContentionGateActiveCodeCallSymbolThreeLegPartitionDedupes(t *testing.T) {
	ctx, database := openActiveCodeCallSymbolContentSchema(t)
	now := time.Now().UTC()
	const javaKey = "scip-java maven org.acme/core org.acme/Thing#run()."
	const goKey = "scip-go gomod github.com/acme/lib New()."

	seedActiveCodeCallSymbolRepositoryScope(t, ctx, database, "scope:logging", "repository:r_logging", "generation-logging", now)
	seedActiveCodeCallSymbolManifest(t, ctx, database, "repository:r_logging", "package.json", `{"name":"@acme/logging"}`, now)
	seedActiveCodeCallSymbolPackageFact(t, ctx, database, "fact-logging", "scope:logging", "generation-logging", "src/logger.js", "@acme/logging", "Logger", now)

	seedActiveCodeCallSymbolRepositoryScope(t, ctx, database, "scope:lib", "repository:r_lib", "generation-lib", now)
	seedActiveCodeCallSymbolGoMod(t, ctx, database, "repository:r_lib", "go.mod", "github.com/acme/lib", now)
	seedActiveCodeCallSymbolFact(t, ctx, database, "fact-lib", "scope:lib", "generation-lib", "lib.go", goKey, now.Add(time.Second))
	// One file that defines both a Go key and a key only the corpus scan serves.
	if _, err := database.ExecContext(ctx, `
UPDATE fact_records SET payload = jsonb_set(payload, '{parsed_file_data,functions}',
    (payload->'parsed_file_data'->'functions') || jsonb_build_array(jsonb_build_object('uid', 'uid:java', 'scip_symbol', $1::text)))
WHERE fact_id = 'fact-lib'`, javaKey); err != nil {
		t.Fatalf("add second definition: %v", err)
	}
	seedActiveCodeCallSymbolRepositoryScope(t, ctx, database, "scope:java", "repository:r_java", "generation-java", now)
	seedActiveCodeCallSymbolFact(t, ctx, database, "fact-java", "scope:java", "generation-java", "Thing.java", javaKey, now.Add(2*time.Second))

	queryer := &recordingCodeCallSymbolQueryer{SQLDB: SQLDB{DB: database}}
	loaded, err := NewFactStore(queryer).LoadActiveCodeCallSymbolDefinitionFacts(ctx, []string{javaKey, goKey, "package:@acme/logging#Logger"})
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts() error = %v, want nil", err)
	}
	if got, want := sortedFactIDs(loaded), []string{"fact-java", "fact-lib", "fact-logging"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded = %v, want %v (fact-lib matches both the corpus scan and the Go leg, once)", got, want)
	}
	if got, want := queryer.queries, []string{
		listActiveCodeCallSymbolDefinitionFactsQuery,
		producerstore.PackageManifestsQuery,
		listAnchoredActiveCodeCallSymbolDefinitionFactsQuery,
		producerstore.GoModuleManifestsQuery,
		listAnchoredActiveCodeCallSymbolDefinitionFactsQuery,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("issued %d statements, want corpus scan, package manifest read and anchored scan, go.mod read and anchored scan", len(got))
	}
}
