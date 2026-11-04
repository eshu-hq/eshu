// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"strings"
	"testing"
)

// deltaOverlayShapeActive is a minimal active reader text: one
// FROM-shaped fact_records branch with the two swapped fragments and the
// given extra predicates.
func deltaOverlayShapeActive(extra string) string {
	return `
SELECT fact.fact_id
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'file'
  AND generation.status = 'active'
` + extra
}

// TestDeriveDeltaOverlayBaseQueryBindsTheScopePlaceholder pins the
// placeholder parametrization the #10003 readers need: Crossplane XRD binds
// the scope ids at $4, code-call at $5 and $6, JVM at $6, terraform at $1 and
// $2. The derived text names the requested placeholder once per branch and no
// other scope placeholder.
func TestDeriveDeltaOverlayBaseQueryBindsTheScopePlaceholder(t *testing.T) {
	t.Parallel()

	for _, param := range []int{1, 2, 4, 5, 6} {
		var extra strings.Builder
		for used := 1; used < param; used++ {
			fmt.Fprintf(&extra, "  AND fact.fact_id <> $%d::text\n", used)
		}
		derived, err := deriveDeltaOverlayBaseQuery(deltaOverlayShapeActive(extra.String()), param)
		if err != nil {
			t.Fatalf("$%d: derive: %v", param, err)
		}
		want := fmt.Sprintf("AND fact.scope_id = ANY($%d::text[])", param)
		if got := strings.Count(derived, want); got != 1 {
			t.Fatalf("$%d: derived carries %q %d times, want 1:\n%s", param, want, got, derived)
		}
		if got := strings.Count(derived, "fact.scope_id = ANY("); got != 1 {
			t.Fatalf("$%d: derived carries %d scope filters, want 1:\n%s", param, got, derived)
		}
		if strings.Contains(derived, deltaOverlayActiveScopeJoin) || strings.Contains(derived, deltaOverlayActiveGeneration) {
			t.Fatalf("$%d: derived keeps an active-generation fragment:\n%s", param, derived)
		}
		if got := strings.Count(derived, deltaOverlayBaseJoin); got != 1 {
			t.Fatalf("$%d: derived carries the base join %d times, want 1", param, got)
		}
	}
}

// TestDeriveDeltaOverlayBaseQueryKeepsTheManifestText pins the manifest
// reader's derived text byte for byte against the #9991 derivation, which
// hardcoded $6: the generic helper must not change the manifest reader's SQL.
// The four fragments are the #9991 manifestActiveScopeJoin,
// manifestDeltaBaseJoin, manifestActiveGeneration, and manifestDeltaBaseFull
// constants (git show c9c1f40375:go/internal/storage/postgres/active_package_manifest_dependency_delta.go),
// frozen here as the pre-refactor oracle.
func TestDeriveDeltaOverlayBaseQueryKeepsTheManifestText(t *testing.T) {
	t.Parallel()

	const (
		joinBefore = "AND scope.active_generation_id = fact.generation_id"
		joinAfter  = "AND scope.active_generation_id <> fact.generation_id\nJOIN scope_generations AS active_delta\n  ON active_delta.scope_id = scope.scope_id\n AND active_delta.generation_id = scope.active_generation_id"
		genBefore  = "AND generation.status = 'active'"
		genAfter   = "AND generation.is_delta = false\n  AND generation.activated_at IS NOT NULL\n  AND generation.source_commit_sha = COALESCE(NULLIF(TRIM(active_delta.delta_union_base_commit_sha), ''), NULLIF(TRIM(active_delta.delta_baseline_commit_sha), ''))\n  AND active_delta.is_delta = true\n  AND active_delta.status = 'active'\n  AND fact.scope_id = ANY($6::text[])"
	)
	for _, active := range []string{
		listActivePackageManifestDependencyFactsQuery,
		listActivePackageManifestDependencyFactsWithSBTQuery,
	} {
		want := strings.ReplaceAll(strings.ReplaceAll(active, joinBefore, joinAfter), genBefore, genAfter)
		got, err := deriveDeltaOverlayBaseQuery(active, manifestDeltaOverlayScopeParam)
		if err != nil {
			t.Fatalf("derive: %v", err)
		}
		if got != want {
			t.Fatalf("derived manifest text changed:\n got: %s\nwant: %s", got, want)
		}
	}
}

// TestDeriveDeltaOverlayBaseQueryDerivesAJoinShapedReader pins the relaxed
// shape guard: a reader whose fact branch is JOIN fact_records AS fact, as
// listTerraformBackendFactsQuery is, derives. Another fact_records alias in
// the same text (fact_records AS repository) is not a branch.
func TestDeriveDeltaOverlayBaseQueryDerivesAJoinShapedReader(t *testing.T) {
	t.Parallel()

	active := `
WITH requested AS (
    SELECT repository.payload->>'repo_id' AS repo_id
    FROM unnest($1::text[]) AS value
    JOIN fact_records AS repository
      ON repository.fact_kind = 'repository'
    JOIN ingestion_scopes AS repository_scope
      ON repository_scope.scope_id = repository.scope_id
     AND repository_scope.active_generation_id = repository.generation_id
    JOIN scope_generations AS repository_generation
      ON repository_generation.generation_id = repository.generation_id
    WHERE repository_generation.status = 'active'
)
SELECT requested.repo_id, fact.payload
FROM requested
JOIN fact_records AS fact
  ON fact.payload->>'repo_id' = requested.repo_id
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'file'
  AND generation.status = 'active'
ORDER BY requested.repo_id, fact.observed_at, fact.fact_id
`
	derived, err := deriveDeltaOverlayBaseQuery(active, 2)
	if err != nil {
		t.Fatalf("derive JOIN-shaped reader: %v", err)
	}
	if got := strings.Count(derived, "AND fact.scope_id = ANY($2::text[])"); got != 1 {
		t.Fatalf("derived carries the $2 scope filter %d times, want 1:\n%s", got, derived)
	}
	for _, kept := range []string{
		"AND repository_scope.active_generation_id = repository.generation_id",
		"WHERE repository_generation.status = 'active'",
	} {
		if !strings.Contains(derived, kept) {
			t.Fatalf("derived dropped the repository branch fragment %q:\n%s", kept, derived)
		}
	}
}

// TestDeriveDeltaOverlayBaseQueryFailsClosed pins every shape the derivation
// cannot derive safely: it must refuse rather than return a text that reads
// the wrong generations or binds the scope ids onto an existing parameter.
func TestDeriveDeltaOverlayBaseQueryFailsClosed(t *testing.T) {
	t.Parallel()

	valid := deltaOverlayShapeActive("")
	for name, cell := range map[string]struct {
		active string
		param  int
	}{
		"no fact branch":                {"SELECT 1", 1},
		"aliased fact_records only":     {strings.ReplaceAll(valid, "fact_records AS fact\n", "fact_records AS fact_row\n"), 1},
		"missing active scope join":     {strings.Replace(valid, deltaOverlayActiveScopeJoin, "", 1), 1},
		"missing active generation":     {strings.Replace(valid, deltaOverlayActiveGeneration, "", 1), 1},
		"second branch without join":    {valid + "UNION ALL\nSELECT fact.fact_id FROM fact_records AS fact\n" + deltaOverlayActiveGeneration + "\n", 1},
		"duplicate scope join":          {strings.Replace(valid, deltaOverlayActiveScopeJoin, deltaOverlayActiveScopeJoin+"\n "+deltaOverlayActiveScopeJoin, 1), 1},
		"placeholder already bound":     {deltaOverlayShapeActive("  AND fact.fact_id <> $3::text\n"), 3},
		"placeholder zero":              {valid, 0},
		"placeholder negative":          {valid, -1},
		"active_delta alias already in": {strings.Replace(valid, "WHERE", "JOIN scope_generations AS active_delta ON true\nWHERE", 1), 1},
	} {
		if derived, err := deriveDeltaOverlayBaseQuery(cell.active, cell.param); err == nil {
			t.Fatalf("%s: derive error = nil, want the unsupported-shape error; derived:\n%s", name, derived)
		}
	}
	// A longer placeholder that shares the digits does not collide: $10 is
	// not $1.
	if _, err := deriveDeltaOverlayBaseQuery(deltaOverlayShapeActive("  AND fact.fact_id <> $10::text\n"), 1); err != nil {
		t.Fatalf("derive with $10 bound and $1 requested: %v, want nil", err)
	}
}
