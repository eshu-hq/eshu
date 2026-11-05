// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10003 row 2 disposable PostgreSQL proof that ListActiveJVMReachabilityFacts overlays a delta-active git scope on its #7797 base full generation: the unchanged Main.java import evidence of the base stays visible to the supply-chain impact pass; reads ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE

import (
	"slices"
	"strings"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// seedJVMFile seeds one file fact with JVM language and import evidence. key
// is the stable fact key the delta overlay matches on.
func seedJVMFile(t *testing.T, f *deltaManifestFixture, scopeID, generationID, factID, key, relativePath, lang, imported string) {
	t.Helper()
	f.exec("seed JVM fact "+factID, `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
     source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, 'file', $4,
        'git', $4, clock_timestamp(), clock_timestamp(), FALSE,
        jsonb_build_object(
            'repo_id', $2::text,
            'relative_path', $5::text,
            'parsed_file_data', jsonb_build_object(
                'lang', $6::text,
                'imports', jsonb_build_array(jsonb_build_object('source', $7::text))
            )
        ))`,
		factID, scopeID, generationID, key, relativePath, lang, imported)
}

// seedJVMBase seeds scopeID with a full generation at commit c0 holding
// Main.java (importing com.acme.api) and an untouched helper, activated.
func seedJVMBase(f *deltaManifestFixture, scopeID, fullID string) {
	f.t.Helper()
	f.scope(scopeID)
	f.generation(scopeID, deltaManifestGeneration{id: fullID, commit: "c0"})
	seedJVMFile(f.t, f, scopeID, fullID, "full-main", "file:"+scopeID+":src/Main.java",
		"src/Main.java", "java", "com.acme.api.Client")
	seedJVMFile(f.t, f, scopeID, fullID, "full-helper", "file:"+scopeID+":src/Helper.java",
		"src/Helper.java", "java", "com.acme.util.Helper")
	f.activate(scopeID, fullID)
}

func jvmReachabilityFilter(scopeID string) supplychaincore.JVMReachabilityFactFilter {
	return supplychaincore.JVMReachabilityFactFilter{
		RepositoryIDs: []string{scopeID},
		APIPackages:   []string{"com.acme.api"},
	}
}

func loadJVMIDs(t *testing.T, f *deltaManifestFixture, scopeID string) []string {
	t.Helper()
	loaded, err := f.store.ListActiveJVMReachabilityFacts(f.ctx, jvmReachabilityFilter(scopeID))
	if err != nil {
		t.Fatalf("ListActiveJVMReachabilityFacts(): %v", err)
	}
	ids := make([]string, 0, len(loaded))
	for _, envelope := range loaded {
		ids = append(ids, envelope.FactID)
	}
	slices.Sort(ids)
	return ids
}

// TestListActiveJVMReachabilityFactsDeltaActiveScopeLive is the #10003 row 2
// regression. A full generation holds Main.java importing com.acme.api; a
// delta generation touches only the helper, re-emits it, and becomes the
// scope's active generation. The impact pass matches API packages through
// this loader, so the unchanged Main.java evidence must stay visible. Before
// the fix the read joined only the active generation and returned no rows,
// so a finding kept unknown reachability instead of jvm_package_api_reachable.
func TestListActiveJVMReachabilityFactsDeltaActiveScopeLive(t *testing.T) {
	f := openDeltaManifestFixture(t)
	seedJVMBase(f, "jvm-delta-scope", "jvm-full-generation")
	f.generation("jvm-delta-scope", deltaManifestGeneration{
		id: "jvm-live-delta", isDelta: true, commit: "c1", baseline: "c0", unionBase: "c0",
	})
	f.deltaRepository("jvm-delta-scope", "jvm-live-delta", []string{"src/Helper.java"}, nil)
	seedJVMFile(f.t, f, "jvm-delta-scope", "jvm-live-delta", "delta-helper",
		"file:jvm-delta-scope:src/Helper.java", "src/Helper.java", "java", "com.acme.util.Helper")
	f.activate("jvm-delta-scope", "jvm-live-delta")

	got := loadJVMIDs(t, f, "jvm-delta-scope")
	t.Logf("loaded JVM reachability facts for delta-active scope = %v", got)
	want := []string{"full-main"}
	if !slices.Equal(got, want) {
		t.Fatalf("loaded JVM reachability facts = %v, want %v", got, want)
	}
}

// TestListActiveJVMReachabilityFactsDeltaStatementsLive proves the fixed read
// issues the overlay statements on a delta-active scope: the active scan,
// one delta-target lookup, the derived base scan, and the overlay inputs
// read.
func TestListActiveJVMReachabilityFactsDeltaStatementsLive(t *testing.T) {
	f := openDeltaManifestFixture(t)
	seedJVMBase(f, "jvm-stmt-scope", "jvm-stmt-full")
	f.generation("jvm-stmt-scope", deltaManifestGeneration{
		id: "jvm-stmt-delta", isDelta: true, commit: "c1", baseline: "c0", unionBase: "c0",
	})
	f.deltaRepository("jvm-stmt-scope", "jvm-stmt-delta", []string{"src/Helper.java"}, nil)
	seedJVMFile(f.t, f, "jvm-stmt-scope", "jvm-stmt-delta", "delta-stmt-helper",
		"file:jvm-stmt-scope:src/Helper.java", "src/Helper.java", "java", "com.acme.util.Helper")
	f.activate("jvm-stmt-scope", "jvm-stmt-delta")

	counter := &overlayCountingQueryer{SQLDB: SQLDB{DB: f.database}}
	store := NewFactStore(counter)
	loaded, err := store.ListActiveJVMReachabilityFacts(f.ctx, jvmReachabilityFilter("jvm-stmt-scope"))
	if err != nil {
		t.Fatalf("ListActiveJVMReachabilityFacts(): %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded %d facts, want 1 (overlaid base Main.java)", len(loaded))
	}
	tally := tallyJVMStatements(t, counter.queries)
	t.Logf("delta-active statements: active=%d targets=%d base=%d resolve=%d inputs=%d other=%d",
		tally.active, tally.targets, tally.base, tally.resolve, tally.inputs, tally.other)
	if tally.active != 1 || tally.targets != 1 || tally.base != 1 || tally.resolve != 2 || tally.inputs != 1 || tally.other != 0 {
		t.Fatalf("statement tally = %+v, want active=1 targets=1 base=1 resolve=2 inputs=1 other=0", tally)
	}
}

// TestListActiveJVMReachabilityFactsNoDeltaStatementsLive proves a read with
// no delta-active scope gains no base or inputs statement: only the active
// scan and the single delta-target lookup run.
func TestListActiveJVMReachabilityFactsNoDeltaStatementsLive(t *testing.T) {
	f := openDeltaManifestFixture(t)
	seedJVMBase(f, "jvm-nodelta-scope", "jvm-nodelta-full")

	counter := &overlayCountingQueryer{SQLDB: SQLDB{DB: f.database}}
	store := NewFactStore(counter)
	loaded, err := store.ListActiveJVMReachabilityFacts(f.ctx, jvmReachabilityFilter("jvm-nodelta-scope"))
	if err != nil {
		t.Fatalf("ListActiveJVMReachabilityFacts(): %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded %d facts, want 1", len(loaded))
	}
	tally := tallyJVMStatements(t, counter.queries)
	t.Logf("no-delta statements: active=%d targets=%d base=%d resolve=%d inputs=%d other=%d",
		tally.active, tally.targets, tally.base, tally.resolve, tally.inputs, tally.other)
	if tally.active != 1 || tally.targets != 1 || tally.base != 0 || tally.resolve != 0 || tally.inputs != 0 || tally.other != 0 {
		t.Fatalf("statement tally = %+v, want active=1 targets=1 base=0 resolve=0 inputs=0 other=0", tally)
	}
}

// tallyJVMStatements classifies the counted statements of one JVM read. The
// derived base text is identified by the active_delta join the derivation
// introduces; the active text never carries that alias (the derivation fails
// closed when it does).
func tallyJVMStatements(t *testing.T, queries []string) overlayStatementTally {
	t.Helper()
	var tally overlayStatementTally
	for _, query := range queries {
		switch {
		case query == deltaOverlayTargetsQuery:
			tally.targets++
		case query == deltaOverlayInputsQuery:
			tally.inputs++
		case strings.Contains(query, "active_delta"):
			tally.base++
		case strings.Contains(query, "unnest($1::text[], $2::text[])"):
			tally.resolve++
		case query == listActiveJVMReachabilityFactsQuery:
			tally.active++
		default:
			tally.other++
			t.Logf("unclassified statement: %.120q", query)
		}
	}
	return tally
}
