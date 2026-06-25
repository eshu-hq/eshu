// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"
	"time"
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
	})

	// Use a cross-repo source-run ID — only this variant triggers the
	// relationship_generations activation check.
	key := SharedProjectionAcceptanceKey{ScopeID: "scope-1", AcceptanceUnitID: "repo-a", SourceRunID: "repo_dependency:scope-1"}

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
	)

	if gen, ok := gated(SharedProjectionAcceptanceKey{AcceptanceUnitID: "repo-a", SourceRunID: "repo_dependency:scope-1"}); ok {
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
	)

	if gen, ok := gated(SharedProjectionAcceptanceKey{ScopeID: "s", AcceptanceUnitID: "repo-a", SourceRunID: "repo_dependency:scope-a"}); ok {
		t.Fatalf("gated lookup = (%q, true) on error, want deferred (\"\", false)", gen)
	}
}

// TestGateAcceptedGenerationPrefetchOnActiveDefersUntilActive proves the same
// fence on the batched prefetch path used by the repo-dependency runner.
func TestGateAcceptedGenerationPrefetchOnActiveDefersUntilActive(t *testing.T) {
	t.Parallel()

	basePrefetch := func(_ context.Context, _ []SharedProjectionIntentRow) (AcceptedGenerationLookup, error) {
		return acceptedGenerationFixed("gen-2", true), nil
	}
	active := false
	gatedPrefetch := GateAcceptedGenerationPrefetchOnActive(basePrefetch, func(string) (bool, error) {
		return active, nil
	})

	lookup, err := gatedPrefetch(context.Background(), nil)
	if err != nil {
		t.Fatalf("gated prefetch error = %v", err)
	}
	// Use a cross-repo source-run ID so the prefetch gate is exercised.
	key := SharedProjectionAcceptanceKey{ScopeID: "s", AcceptanceUnitID: "repo-a", SourceRunID: "repo_dependency:s"}
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

	basePrefetch := func(_ context.Context, _ []SharedProjectionIntentRow) (AcceptedGenerationLookup, error) {
		return acceptedGenerationFixed("gen-2", true), nil
	}
	checks := 0
	gatedPrefetch := GateAcceptedGenerationPrefetchOnActive(basePrefetch, func(string) (bool, error) {
		checks++
		return true, nil
	})

	lookup, err := gatedPrefetch(context.Background(), nil)
	if err != nil {
		t.Fatalf("gated prefetch error = %v", err)
	}
	for i := 0; i < 5; i++ {
		// Cross-repo source run so the active check is actually invoked.
		key := SharedProjectionAcceptanceKey{ScopeID: "s", AcceptanceUnitID: "repo-a", SourceRunID: "repo_dependency:s"}
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
	)

	// "code_import_repo_dependency:<scope>" is the source-run form for
	// code-import intents. The gate must NOT apply the activation check.
	key := SharedProjectionAcceptanceKey{
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
	)
	key := SharedProjectionAcceptanceKey{
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
	)
	key := SharedProjectionAcceptanceKey{
		ScopeID:          "package-registry-scope:pkg-scope",
		AcceptanceUnitID: "repository:r_consumer",
		SourceRunID:      "package_consumption_repo_dependency:package-registry-scope:pkg-scope",
	}
	gen, ok := gated(key)
	if !ok || gen != "scope-gen-xyz" {
		t.Fatalf("gated lookup = (%q, %v), want (scope-gen-xyz, true) for package-consumption source run", gen, ok)
	}
}

// TestRepoDependencyRunnerDefersGraphWriteUntilGenerationActive proves the
// end-to-end fence at the runner: with acceptance committed for the intent's
// generation but that generation NOT yet active, the repo-dependency runner
// writes NO graph edges and processes no intents; once the generation is
// activated, the next cycle projects the edges.
func TestRepoDependencyRunnerDefersGraphWriteUntilGenerationActive(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 22, 12, 0, 0, 0, time.UTC)
	repoID := "repository:r_repo_a"
	intent := repoDependencyIntentRow(
		"active-1", "scope-b", repoID, repoID, "repo_dependency:scope-b", "gen-2", now,
		map[string]any{
			"repo_id":           repoID,
			"target_repo_id":    "repository:r_target_1",
			"relationship_type": "DEPENDS_ON",
			"evidence_source":   crossRepoEvidenceSource,
		},
	)
	reader := &fakeRepoDependencyIntentStore{
		pendingByDomain: []SharedProjectionIntentRow{intent},
		pendingByAcceptanceUnit: map[string][]SharedProjectionIntentRow{
			repoID: {intent},
		},
		leaseGranted: true,
	}
	writer := &recordingCodeCallProjectionEdgeWriter{}

	active := false
	gated := GateAcceptedGenerationOnActive(
		acceptedGenerationFixed("gen-2", true),
		func(string) (bool, error) { return active, nil },
	)
	runner := RepoDependencyProjectionRunner{
		IntentReader: reader,
		LeaseManager: reader,
		EdgeWriter:   writer,
		AcceptedGen:  gated,
		Config:       RepoDependencyProjectionRunnerConfig{PollInterval: 10 * time.Millisecond},
	}

	result, err := runner.processOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("processOnce() (inactive) error = %v", err)
	}
	if result.ProcessedIntents != 0 {
		t.Fatalf("ProcessedIntents = %d before activation, want 0", result.ProcessedIntents)
	}
	if len(writer.writeCalls) != 0 {
		t.Fatalf("graph write calls = %d before activation, want 0", len(writer.writeCalls))
	}

	active = true
	result, err = runner.processOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("processOnce() (active) error = %v", err)
	}
	if result.ProcessedIntents != 1 {
		t.Fatalf("ProcessedIntents = %d after activation, want 1", result.ProcessedIntents)
	}
	if len(writer.writeCalls) != 1 {
		t.Fatalf("graph write calls = %d after activation, want 1", len(writer.writeCalls))
	}
}
