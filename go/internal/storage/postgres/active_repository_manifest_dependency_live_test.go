// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10660 repository-scoped declared manifest dependency read: a two-manifest repository returns both files' rows in (observed_at, fact_id) order across keyset pages; runtime dependencies are the default and dev/test appear only with includeDev; an empty-manifest repository returns an empty list with freshness state, not an error; the read overlays a delta-active scope exactly like the reducer's pair-probe read; reads ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE

import (
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// seedTwoManifestRepo seeds scopeID with an active full generation holding
// runtime pins in package.json and dev pins in packages/dev/package.json.
func seedTwoManifestRepo(f *deltaManifestFixture, scopeID string, runtime, dev int) {
	f.t.Helper()
	f.scope(scopeID)
	f.generation(scopeID, deltaManifestGeneration{id: scopeID + "-full", commit: "c0"})
	for i := 0; i < runtime; i++ {
		f.pin(scopeID, scopeID+"-full", deltaManifestPin{
			factID:  fmt.Sprintf("%s-rt-%03d", scopeID, i),
			key:     fmt.Sprintf("content_entity:%s:runtime:%03d", scopeID, i),
			name:    fmt.Sprintf("runtime-pkg-%03d", i),
			path:    "package.json",
			version: "1.0.0",
		})
	}
	for i := 0; i < dev; i++ {
		f.pin(scopeID, scopeID+"-full", deltaManifestPin{
			factID:          fmt.Sprintf("%s-dev-%03d", scopeID, i),
			key:             fmt.Sprintf("content_entity:%s:dev:%03d", scopeID, i),
			name:            fmt.Sprintf("dev-pkg-%03d", i),
			path:            "packages/dev/package.json",
			version:         "^2.0.0",
			development:     true,
			dependencyScope: "dev",
		})
	}
	f.activate(scopeID, scopeID+"-full")
}

// collectRepositoryManifestPage walks the scoped read to completion with the
// given fetch limit and dev-inclusion flag, asserting the (observed_at,
// fact_id) order contract on every page edge.
func collectRepositoryManifestPage(t *testing.T, f *deltaManifestFixture, scopeID string, fetchLimit int, includeDev bool) []facts.Envelope {
	t.Helper()
	var all []facts.Envelope
	var cursorObservedAt *time.Time
	var cursorFactID string
	for {
		page, freshness, refused, err := f.store.ListActiveRepositoryManifestDependencyFacts(
			f.ctx, scopeID, cursorObservedAt, cursorFactID, fetchLimit, includeDev)
		if err != nil {
			t.Fatalf("ListActiveRepositoryManifestDependencyFacts(%s): %v", scopeID, err)
		}
		if refused {
			t.Fatalf("ListActiveRepositoryManifestDependencyFacts(%s): refused an intact base", scopeID)
		}
		if !freshness.ScopeFound {
			t.Fatalf("ListActiveRepositoryManifestDependencyFacts(%s): scope not found", scopeID)
		}
		all = append(all, page...)
		if len(page) < fetchLimit {
			return all
		}
		last := page[len(page)-1]
		observedAt := last.ObservedAt.UTC()
		cursorObservedAt = &observedAt
		cursorFactID = last.FactID
	}
}

// assertObservedFactIDOrder fails when envelopes leave (observed_at, fact_id)
// order.
func assertObservedFactIDOrder(t *testing.T, envelopes []facts.Envelope) {
	t.Helper()
	for i := 1; i < len(envelopes); i++ {
		prev, cur := envelopes[i-1], envelopes[i]
		if cur.ObservedAt.Before(prev.ObservedAt) ||
			(cur.ObservedAt.Equal(prev.ObservedAt) && cur.FactID < prev.FactID) {
			t.Fatalf("row %d out of (observed_at, fact_id) order: %s after %s", i, cur.FactID, prev.FactID)
		}
	}
}

// TestListActiveRepositoryManifestDependencyFactsTwoManifestsLive seeds one
// repository with two manifest files and asserts the scoped read returns both
// files' rows in (observed_at, fact_id) order, paging forward with the
// keyset cursor without gaps or duplicates.
func TestListActiveRepositoryManifestDependencyFactsTwoManifestsLive(t *testing.T) {
	f := openDeltaManifestFixture(t)
	const scopeID = "repo-two-manifests"
	seedTwoManifestRepo(f, scopeID, 60, 30)

	full, _, refused, err := f.store.ListActiveRepositoryManifestDependencyFacts(f.ctx, scopeID, nil, "", 1000, true)
	if err != nil {
		t.Fatalf("ListActiveRepositoryManifestDependencyFacts: %v", err)
	}
	if refused {
		t.Fatal("refused an intact base")
	}
	if len(full) != 90 {
		t.Fatalf("rows = %d, want 90 (60 runtime + 30 dev)", len(full))
	}
	assertObservedFactIDOrder(t, full)
	paths := map[string]int{}
	for _, env := range full {
		paths[env.Payload["relative_path"].(string)]++
		if env.ScopeID != scopeID {
			t.Fatalf("row %s carries scope %s, want %s", env.FactID, env.ScopeID, scopeID)
		}
	}
	if paths["package.json"] != 60 || paths["packages/dev/package.json"] != 30 {
		t.Fatalf("per-file rows = %v, want map[package.json:60 packages/dev/package.json:30]", paths)
	}

	paged := collectRepositoryManifestPage(t, f, scopeID, 26, true)
	if len(paged) != len(full) {
		t.Fatalf("paged rows = %d, want %d", len(paged), len(full))
	}
	for i := range full {
		if paged[i].FactID != full[i].FactID {
			t.Fatalf("paged row %d = %s, want %s: cursor walk skipped or duplicated a row", i, paged[i].FactID, full[i].FactID)
		}
	}
}

// TestListActiveRepositoryManifestDependencyFactsScopeFilterLive seeds one
// repository with runtime and dev pins across two manifest files and asserts
// the #10660 scope filter: runtime dependencies are the default, and dev/test
// dependencies appear only when includeDev is true.
func TestListActiveRepositoryManifestDependencyFactsScopeFilterLive(t *testing.T) {
	f := openDeltaManifestFixture(t)
	const scopeID = "repo-scope-filter"
	seedTwoManifestRepo(f, scopeID, 60, 30)

	runtime, _, refused, err := f.store.ListActiveRepositoryManifestDependencyFacts(f.ctx, scopeID, nil, "", 1000, false)
	if err != nil {
		t.Fatalf("ListActiveRepositoryManifestDependencyFacts(runtime): %v", err)
	}
	if refused {
		t.Fatal("refused an intact base")
	}
	if len(runtime) != 60 {
		t.Fatalf("runtime rows = %d, want 60 (dev excluded by default)", len(runtime))
	}
	for _, env := range runtime {
		if env.Payload["relative_path"] != "package.json" {
			t.Fatalf("runtime row %s carries path %v, want only package.json", env.FactID, env.Payload["relative_path"])
		}
	}

	all, _, refused, err := f.store.ListActiveRepositoryManifestDependencyFacts(f.ctx, scopeID, nil, "", 1000, true)
	if err != nil {
		t.Fatalf("ListActiveRepositoryManifestDependencyFacts(all): %v", err)
	}
	if refused {
		t.Fatal("refused an intact base")
	}
	if len(all) != 90 {
		t.Fatalf("all rows = %d, want 90 (60 runtime + 30 dev)", len(all))
	}
}

// TestListActiveRepositoryManifestDependencyFactsEmptyRepoLive asserts a
// repository with an active generation but no manifest rows returns an empty
// list with freshness state, not an error — and that an unknown scope reports
// scope_found false with the same empty shape.
func TestListActiveRepositoryManifestDependencyFactsEmptyRepoLive(t *testing.T) {
	f := openDeltaManifestFixture(t)
	const scopeID = "repo-no-manifest"
	f.scope(scopeID)
	f.generation(scopeID, deltaManifestGeneration{id: scopeID + "-full", commit: "c0"})
	f.activate(scopeID, scopeID+"-full")

	page, freshness, refused, err := f.store.ListActiveRepositoryManifestDependencyFacts(f.ctx, scopeID, nil, "", 201, false)
	if err != nil {
		t.Fatalf("ListActiveRepositoryManifestDependencyFacts: %v", err)
	}
	if refused {
		t.Fatal("refused an intact base")
	}
	if len(page) != 0 {
		t.Fatalf("rows = %d, want 0 for a repository with no manifest", len(page))
	}
	if !freshness.ScopeFound {
		t.Fatal("freshness.ScopeFound = false, want true for a known scope")
	}
	if freshness.ActiveGenerationID != scopeID+"-full" {
		t.Fatalf("freshness.ActiveGenerationID = %q, want %q", freshness.ActiveGenerationID, scopeID+"-full")
	}
	if freshness.GenerationStatus != "active" {
		t.Fatalf("freshness.GenerationStatus = %q, want active", freshness.GenerationStatus)
	}

	absent, absentFreshness, absentRefused, err := f.store.ListActiveRepositoryManifestDependencyFacts(f.ctx, "repo-absent", nil, "", 201, false)
	if err != nil {
		t.Fatalf("ListActiveRepositoryManifestDependencyFacts(absent): %v", err)
	}
	if absentRefused {
		t.Fatal("refused an absent scope")
	}
	if len(absent) != 0 {
		t.Fatalf("absent rows = %d, want 0", len(absent))
	}
	if absentFreshness.ScopeFound {
		t.Fatal("absent freshness.ScopeFound = true, want false")
	}
}

// TestListActiveRepositoryManifestDependencyFactsDeltaParityLive asserts the
// scoped read overlays a delta-active scope exactly like the reducer's
// pair-probe read: the same rows in the same order.
func TestListActiveRepositoryManifestDependencyFactsDeltaParityLive(t *testing.T) {
	f := openDeltaManifestFixture(t)
	const scopeID = "repo-delta-parity"
	seedTwoManifestRepo(f, scopeID, 20, 10)
	delta := scopeID + "-delta"
	f.generation(scopeID, deltaManifestGeneration{id: delta, isDelta: true, commit: "c1", baseline: "c0", unionBase: "c0"})
	f.deltaRepository(scopeID, delta, []string{"package.json"}, nil)
	f.pin(scopeID, delta, deltaManifestPin{factID: delta + "-reemit", key: fmt.Sprintf("content_entity:%s:runtime:000", scopeID), name: "runtime-pkg-000", path: "package.json", version: "1.0.1"})
	f.pin(scopeID, delta, deltaManifestPin{factID: delta + "-tomb", key: fmt.Sprintf("content_entity:%s:dev:000", scopeID), name: "dev-pkg-000", path: "packages/dev/package.json", version: "^2.0.0", tombstone: true})
	f.pin(scopeID, delta, deltaManifestPin{factID: delta + "-new", key: fmt.Sprintf("content_entity:%s:new", scopeID), name: "new-pkg", path: "package.json", version: "3.0.0"})
	f.activate(scopeID, delta)
	f.exec("analyze", "ANALYZE fact_records; ANALYZE scope_generations; ANALYZE ingestion_scopes")

	got, _, refused, err := f.store.ListActiveRepositoryManifestDependencyFacts(f.ctx, scopeID, nil, "", 1000, true)
	if err != nil {
		t.Fatalf("ListActiveRepositoryManifestDependencyFacts: %v", err)
	}
	if refused {
		t.Fatal("refused an intact delta base")
	}

	managers, query := activePackageManifestDependencyQuery([]string{"npm"})
	probed := []string{"runtime-pkg-000", "dev-pkg-000", "new-pkg"}
	wantAll, _, err := f.store.listManifestDependencyFactsWithQuery(f.ctx, time.Now(), managers, query, probed, false)
	if err != nil {
		t.Fatalf("pair-probe read: %v", err)
	}
	var want []facts.Envelope
	for _, env := range wantAll {
		if env.ScopeID == scopeID {
			want = append(want, env)
		}
	}
	probedSet := map[string]bool{}
	for _, name := range probed {
		probedSet[name] = true
	}
	byID := map[string]facts.Envelope{}
	for _, env := range got {
		name, _ := env.Payload["entity_name"].(string)
		if probedSet[name] {
			byID[env.FactID] = env
		}
	}
	for _, env := range want {
		match, ok := byID[env.FactID]
		if !ok {
			t.Fatalf("pair-probe row %s missing from the scoped read", env.FactID)
		}
		if !manifestParityRowEqual(match, env) {
			t.Fatalf("scoped row %s differs from the pair-probe row:\n got %+v\nwant %+v", env.FactID, match, env)
		}
		delete(byID, env.FactID)
	}
	for id := range byID {
		t.Fatalf("scoped row %s has no pair-probe counterpart", id)
	}
}
