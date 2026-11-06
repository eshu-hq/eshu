// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10182 disposable PostgreSQL proof that a delta which drops one of two publishers of one anchored key group keeps the dropped publisher's base definitions visible: the anchored base scan binds the active-only producer set and the dropped scope's base rows go unread; reads ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE

import (
	"bytes"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestLoadActiveCodeCallSymbolDefinitionFactsDroppedPublisherVariantBLive is
// the #10182 regression. Two scopes publish package:alpha-pkg#Widget. Scope
// A's delta renames its manifest (dropping A from active producer discovery)
// while scope B's delta touches an unrelated file (B still anchors the
// group). Pre-fix the anchored base scan binds the active-only set {B}, so
// A's keepable base definition is missing post-overlay; post-fix both base
// definitions load.
func TestLoadActiveCodeCallSymbolDefinitionFactsDroppedPublisherVariantBLive(t *testing.T) {
	f := openDeltaManifestFixture(t)
	const key = "package:alpha-pkg#Widget"
	now := time.Now().UTC()

	scopeA, scopeB := "call-vb-a-scope", "call-vb-b-scope"
	fullA, fullB := "call-vb-a-full", "call-vb-b-full"
	deltaA, deltaB := "call-vb-a-delta", "call-vb-b-delta"

	f.scope(scopeA)
	f.generation(scopeA, deltaManifestGeneration{id: fullA, commit: "c0"})
	seedActiveCodeCallSymbolManifest(t, f.ctx, f.database, scopeA, "package.json", `{"name":"alpha-pkg"}`, fullA, now)
	seedActiveCodeCallSymbolPackageFact(t, f.ctx, f.database, "call-vb-a-base", scopeA, fullA, "src/widget.js", "alpha-pkg", "Widget", now)
	f.activate(scopeA, fullA)
	f.generation(scopeA, deltaManifestGeneration{id: deltaA, isDelta: true, commit: "c1", baseline: "c0", unionBase: "c0"})
	rewriteContentManifest(f, scopeA, deltaA, "package.json", `{"name":"renamed-pkg"}`)
	f.deltaRepository(scopeA, deltaA, []string{"package.json"}, nil)
	f.activate(scopeA, deltaA)

	f.scope(scopeB)
	f.generation(scopeB, deltaManifestGeneration{id: fullB, commit: "c0"})
	seedActiveCodeCallSymbolManifest(t, f.ctx, f.database, scopeB, "package.json", `{"name":"alpha-pkg"}`, fullB, now)
	seedActiveCodeCallSymbolPackageFact(t, f.ctx, f.database, "call-vb-b-base", scopeB, fullB, "src/widget.js", "alpha-pkg", "Widget", now)
	f.activate(scopeB, fullB)
	f.generation(scopeB, deltaManifestGeneration{id: deltaB, isDelta: true, commit: "c1", baseline: "c0", unionBase: "c0"})
	f.deltaRepository(scopeB, deltaB, []string{"src/other.js"}, nil)
	f.activate(scopeB, deltaB)

	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	got := loadCallSymbolIDs(t, f, []string{key})
	t.Logf("loaded call symbol facts for dropped-publisher scopes = %v", got)
	want := []string{"call-vb-a-base", "call-vb-b-base"}
	if !slices.Equal(got, want) {
		t.Fatalf("loaded call symbol facts = %v, want %v (the dropped publisher's keepable base definition is missing)", got, want)
	}
	assertDeltaManifestOutcomes(t,
		f.deltaUnionOutcomesForMetric(t, "eshu_dp_code_call_symbol_definition_read_delta_union_total"),
		map[string]int64{"union_base_found": 2})
	for _, want := range []string{
		"package_producer_scope_count=1",
		"package_union_scope_count=0",
		"package_base_scope_count=2",
	} {
		if !strings.Contains(logged.String(), want) {
			t.Fatalf("load log is missing %q:\n%s", want, logged.String())
		}
	}

	// The widened base pass adds no statement: one active scan, the
	// target lookup, one base page, the resolve pair, and the inputs read.
	counter := &overlayCountingQueryer{SQLDB: SQLDB{DB: f.database}}
	store := NewFactStore(counter)
	loaded, err := store.LoadActiveCodeCallSymbolDefinitionFacts(f.ctx, []string{key})
	if err != nil {
		t.Fatalf("LoadActiveCodeCallSymbolDefinitionFacts(): %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("loaded %d facts, want 2 overlaid base definitions", len(loaded))
	}
	tally := tallyCallSymbolStatements(t, counter.queries)
	t.Logf("dropped-publisher statements: active=%d targets=%d base=%d resolve=%d inputs=%d other=%d",
		tally.active, tally.targets, tally.base, tally.resolve, tally.inputs, tally.other)
	if tally.active != 1 || tally.targets != 1 || tally.base != 1 || tally.resolve != 2 || tally.inputs != 1 || tally.other != 1 {
		t.Fatalf("statement tally = %+v, want active=1 targets=1 base=1 resolve=2 inputs=1 other=1", tally)
	}
}
