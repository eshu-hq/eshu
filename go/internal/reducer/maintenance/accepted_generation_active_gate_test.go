// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package maintenance

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// TestGateAcceptedGenerationOnActiveDefersUntilActive proves the decorator
// withholds graph-projection authority for an accepted generation until the
// relationship generation is activated (published). Acceptance rows alone must
// NOT grant authority, otherwise the graph runner projects edges for a
// generation the Postgres relationship read models do not yet expose
// (graph-ahead-of-Postgres dual-write divergence).
func TestGateAcceptedGenerationOnActiveDefersUntilActive(t *testing.T) {
	t.Parallel()

	base := acceptedGenerationFixed("gen-2", true)
	active := false
	gated := GateAcceptedGenerationOnActive(base, func(generationID string) (bool, error) {
		if generationID != "gen-2" {
			t.Fatalf("isActive called with %q, want gen-2", generationID)
		}
		return active, nil
	}, nil)

	// Use a cross-repo source-run ID — only this variant triggers the
	// relationship_generations activation check.
	key := sharedintent.AcceptanceKey{ScopeID: "scope-1", AcceptanceUnitID: "repo-a", SourceRunID: "repo_dependency:scope-1"}

	// Acceptance committed, generation NOT active yet -> defer (not authoritative).
	if gen, ok := gated(key); ok {
		t.Fatalf("gated lookup = (%q, true) before activation, want deferred (\"\", false)", gen)
	}

	// After activation -> authoritative.
	active = true
	gen, ok := gated(key)
	if !ok || gen != "gen-2" {
		t.Fatalf("gated lookup = (%q, %v) after activation, want (gen-2, true)", gen, ok)
	}
}

// TestGateAcceptedGenerationOnActivePassesThroughMissingAcceptance proves the
// decorator does not invoke the active check when the base acceptance lookup
// has no row, and defers (never fabricates authority).
func TestGateAcceptedGenerationOnActivePassesThroughMissingAcceptance(t *testing.T) {
	t.Parallel()

	called := false
	gated := GateAcceptedGenerationOnActive(
		acceptedGenerationFixed("", false),
		func(string) (bool, error) {
			called = true
			return true, nil
		},
		nil,
	)

	if gen, ok := gated(sharedintent.AcceptanceKey{AcceptanceUnitID: "repo-a", SourceRunID: "repo_dependency:scope-1"}); ok {
		t.Fatalf("gated lookup = (%q, true), want (\"\", false) for missing acceptance", gen)
	}
	if called {
		t.Fatal("active check invoked for missing acceptance row; want skipped")
	}
}

// TestGateAcceptedGenerationOnActiveDefersOnError proves the decorator fails
// safe: if the active check errors, authority is withheld (deferred) rather
// than granted, so a transient lookup failure can never publish graph edges
// ahead of Postgres.
func TestGateAcceptedGenerationOnActiveDefersOnError(t *testing.T) {
	t.Parallel()

	gated := GateAcceptedGenerationOnActive(
		acceptedGenerationFixed("gen-2", true),
		func(string) (bool, error) {
			return false, errors.New("transient lookup failure")
		},
		nil,
	)

	if gen, ok := gated(sharedintent.AcceptanceKey{ScopeID: "s", AcceptanceUnitID: "repo-a", SourceRunID: "repo_dependency:scope-a"}); ok {
		t.Fatalf("gated lookup = (%q, true) on error, want deferred (\"\", false)", gen)
	}
}

// TestGateAcceptedGenerationPrefetchOnActiveDefersUntilActive proves the same
// fence on the batched prefetch path used by the repo-dependency runner.
func TestGateAcceptedGenerationPrefetchOnActiveDefersUntilActive(t *testing.T) {
	t.Parallel()

	basePrefetch := func(_ context.Context, _ []sharedintent.Row) (AcceptedGenerationLookup, error) {
		return acceptedGenerationFixed("gen-2", true), nil
	}
	active := false
	gatedPrefetch := GateAcceptedGenerationPrefetchOnActive(basePrefetch, func(string) (bool, error) {
		return active, nil
	}, nil)

	lookup, err := gatedPrefetch(context.Background(), nil)
	if err != nil {
		t.Fatalf("gated prefetch error = %v", err)
	}
	// Use a cross-repo source-run ID so the prefetch gate is exercised.
	key := sharedintent.AcceptanceKey{ScopeID: "s", AcceptanceUnitID: "repo-a", SourceRunID: "repo_dependency:s"}
	if gen, ok := lookup(key); ok {
		t.Fatalf("prefetched lookup = (%q, true) before activation, want deferred", gen)
	}

	active = true
	lookup, err = gatedPrefetch(context.Background(), nil)
	if err != nil {
		t.Fatalf("gated prefetch error = %v", err)
	}
	if gen, ok := lookup(key); !ok || gen != "gen-2" {
		t.Fatalf("prefetched lookup = (%q, %v) after activation, want (gen-2, true)", gen, ok)
	}
}

// TestGateAcceptedGenerationPrefetchMemoizesActiveCheck proves the prefetch
// gate checks each distinct generation's active status at most once per cycle,
// so the fence does not add a Postgres round trip per intent row on the hot
// selection/filter path.
func TestGateAcceptedGenerationPrefetchMemoizesActiveCheck(t *testing.T) {
	t.Parallel()

	basePrefetch := func(_ context.Context, _ []sharedintent.Row) (AcceptedGenerationLookup, error) {
		return acceptedGenerationFixed("gen-2", true), nil
	}
	checks := 0
	gatedPrefetch := GateAcceptedGenerationPrefetchOnActive(basePrefetch, func(string) (bool, error) {
		checks++
		return true, nil
	}, nil)

	lookup, err := gatedPrefetch(context.Background(), nil)
	if err != nil {
		t.Fatalf("gated prefetch error = %v", err)
	}
	for i := 0; i < 5; i++ {
		// Cross-repo source run so the active check is actually invoked.
		key := sharedintent.AcceptanceKey{ScopeID: "s", AcceptanceUnitID: "repo-a", SourceRunID: "repo_dependency:s"}
		if gen, ok := lookup(key); !ok || gen != "gen-2" {
			t.Fatalf("lookup #%d = (%q, %v), want (gen-2, true)", i, gen, ok)
		}
	}
	if checks != 1 {
		t.Fatalf("active checks = %d across 5 lookups of one generation, want 1 (memoized)", checks)
	}
}

// TestGateAcceptedGenerationOnActivePassesThroughCodeImportSourceRun proves
// that code-import source runs carry scope generation IDs — IDs that are
// NEVER in relationship_generations — and therefore MUST NOT be blocked by the
// activation gate. Before the B-13 fix, GateAcceptedGenerationOnActive applied
// IsGenerationActive uniformly to all repo_dependency intents, permanently
// blocking the 271 code-import intents whose scope gen IDs can never appear in
// relationship_generations.
func TestGateAcceptedGenerationOnActivePassesThroughCodeImportSourceRun(t *testing.T) {
	t.Parallel()

	// isActive returns false for every generation ID, simulating a scope
	// generation ID that will never be found in relationship_generations.
	gated := GateAcceptedGenerationOnActive(
		acceptedGenerationFixed("scope-gen-abc", true),
		func(string) (bool, error) { return false, nil },
		nil,
	)

	// "code_import_repo_dependency:<scope>" is the source-run form for
	// code-import intents. The gate must NOT apply the activation check.
	key := sharedintent.AcceptanceKey{
		ScopeID:          "git-repository-scope:repository:r_app",
		AcceptanceUnitID: "repository:r_app",
		SourceRunID:      "code_import_repo_dependency:git-repository-scope:repository:r_app",
	}
	gen, ok := gated(key)
	if !ok || gen != "scope-gen-abc" {
		t.Fatalf("gated lookup = (%q, %v), want (scope-gen-abc, true) for code-import source run; "+
			"activation gate must not block scope-generation-ID paths", gen, ok)
	}
}

// TestGateAcceptedGenerationOnActivePassesThroughCodeImportBareSourceRun
// covers the bare (no scope suffix) code-import source-run variant.
func TestGateAcceptedGenerationOnActivePassesThroughCodeImportBareSourceRun(t *testing.T) {
	t.Parallel()

	gated := GateAcceptedGenerationOnActive(
		acceptedGenerationFixed("scope-gen-abc", true),
		func(string) (bool, error) { return false, nil },
		nil,
	)
	key := sharedintent.AcceptanceKey{
		ScopeID:          "s",
		AcceptanceUnitID: "repository:r_app",
		SourceRunID:      "code_import_repo_dependency",
	}
	gen, ok := gated(key)
	if !ok || gen != "scope-gen-abc" {
		t.Fatalf("gated lookup = (%q, %v), want (scope-gen-abc, true) for bare code-import source run", gen, ok)
	}
}

// TestGateAcceptedGenerationOnActivePassesThroughPackageConsumptionSourceRun
// proves that package-consumption source runs carry scope generation IDs and
// MUST NOT be blocked by the activation gate — same root cause as the
// code-import case (B-13).
func TestGateAcceptedGenerationOnActivePassesThroughPackageConsumptionSourceRun(t *testing.T) {
	t.Parallel()

	gated := GateAcceptedGenerationOnActive(
		acceptedGenerationFixed("scope-gen-xyz", true),
		func(string) (bool, error) { return false, nil },
		nil,
	)
	key := sharedintent.AcceptanceKey{
		ScopeID:          "package-registry-scope:pkg-scope",
		AcceptanceUnitID: "repository:r_consumer",
		SourceRunID:      "package_consumption_repo_dependency:package-registry-scope:pkg-scope",
	}
	gen, ok := gated(key)
	if !ok || gen != "scope-gen-xyz" {
		t.Fatalf("gated lookup = (%q, %v), want (scope-gen-xyz, true) for package-consumption source run", gen, ok)
	}
}

// TestGateAcceptedGenerationPrefetchPassesThroughCodeImportSourceRun proves
// that the batched prefetch path (the production hot path the repo-dependency
// runner uses) also bypasses the activation gate for code-import source runs.
// GateAcceptedGenerationPrefetchOnActive wraps the resolved lookup with
// GateAcceptedGenerationOnActive, so a regression that reordered the bypass
// relative to the memoized active call would only be caught here.
func TestGateAcceptedGenerationPrefetchPassesThroughCodeImportSourceRun(t *testing.T) {
	t.Parallel()

	basePrefetch := func(_ context.Context, _ []sharedintent.Row) (AcceptedGenerationLookup, error) {
		return acceptedGenerationFixed("scope-gen-ci", true), nil
	}
	// isActive always returns false — simulates a scope generation ID that is
	// never found in relationship_generations.
	gatedPrefetch := GateAcceptedGenerationPrefetchOnActive(basePrefetch, func(string) (bool, error) {
		return false, nil
	}, nil)

	lookup, err := gatedPrefetch(context.Background(), nil)
	if err != nil {
		t.Fatalf("gated prefetch error = %v", err)
	}
	key := sharedintent.AcceptanceKey{
		ScopeID:          "git-repository-scope:repository:r_lib",
		AcceptanceUnitID: "repository:r_lib",
		SourceRunID:      "code_import_repo_dependency:git-repository-scope:repository:r_lib",
	}
	gen, ok := lookup(key)
	if !ok || gen != "scope-gen-ci" {
		t.Fatalf("prefetch lookup = (%q, %v), want (scope-gen-ci, true) for code-import source run; "+
			"activation gate must not block scope-generation-ID paths on the prefetch path", gen, ok)
	}
}

// TestGateAcceptedGenerationPrefetchPassesThroughPackageConsumptionSourceRun
// proves the same bypass on the prefetch path for package-consumption source runs.
func TestGateAcceptedGenerationPrefetchPassesThroughPackageConsumptionSourceRun(t *testing.T) {
	t.Parallel()

	basePrefetch := func(_ context.Context, _ []sharedintent.Row) (AcceptedGenerationLookup, error) {
		return acceptedGenerationFixed("scope-gen-pc", true), nil
	}
	gatedPrefetch := GateAcceptedGenerationPrefetchOnActive(basePrefetch, func(string) (bool, error) {
		return false, nil
	}, nil)

	lookup, err := gatedPrefetch(context.Background(), nil)
	if err != nil {
		t.Fatalf("gated prefetch error = %v", err)
	}
	key := sharedintent.AcceptanceKey{
		ScopeID:          "package-registry-scope:pkg-scope",
		AcceptanceUnitID: "repository:r_consumer",
		SourceRunID:      "package_consumption_repo_dependency:package-registry-scope:pkg-scope",
	}
	gen, ok := lookup(key)
	if !ok || gen != "scope-gen-pc" {
		t.Fatalf("prefetch lookup = (%q, %v), want (scope-gen-pc, true) for package-consumption source run", gen, ok)
	}
}
