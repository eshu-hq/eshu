// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"path"
	"regexp"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/migrations"
)

// The live plan proof for #7088 (TestSupplyChainImpactReadinessRepoArmScopeLive)
// is a scheduled-class test that no CI workflow runs. These guards pin, from
// the production constants and the embedded migration SQL, the statement
// features that proof depends on, so a regression fails in the default test
// run instead of waiting for a scheduled live run. Every needle names SQL code,
// never a phrase that also appears in a SQL comment, so a comment cannot
// satisfy a guard.

const (
	// repoArmLateralFenceNeedle is the closing of a LATERAL probe with its
	// OFFSET 0 fence. The trailing ") AS fact" keeps the SQL comments that
	// mention "OFFSET 0" from satisfying the guard.
	repoArmLateralFenceNeedle = "        OFFSET 0\n    ) AS fact"
	// repoArmScopeAnchorNeedle is the scope anchor that starts each
	// package_manifest_active arm from the repository's own scopes.
	repoArmScopeAnchorNeedle = "    WHERE scope.source_key = $11\n      AND generation.status = 'active'"

	repoArmGapCTEStart = "package_dependency_gap_active AS ("
	repoArmGapCTEEnd   = "unsupported_target_rows AS ("
)

// repoArmGapCTE returns the shipped package_dependency_gap_active CTE text
// cut out of the production readiness statement.
func repoArmGapCTE(t *testing.T) string {
	t.Helper()
	start := strings.Index(ListReadinessQuery, repoArmGapCTEStart)
	end := strings.Index(ListReadinessQuery, repoArmGapCTEEnd)
	if start < 0 || end < start {
		t.Fatalf("ListReadinessQuery lost the %q .. %q CTE boundaries", repoArmGapCTEStart, repoArmGapCTEEnd)
	}
	return ListReadinessQuery[start:end]
}

// TestReadinessRepoArmCTEsAreComposedIntoTheShippedStatement proves the guard
// target is the text the API actually executes, not an orphaned constant.
func TestReadinessRepoArmCTEsAreComposedIntoTheShippedStatement(t *testing.T) {
	t.Parallel()

	if !strings.Contains(ListReadinessQuery, readinessPackageManifestActiveCTE) {
		t.Fatalf("ListReadinessQuery does not embed readinessPackageManifestActiveCTE")
	}
}

// TestReadinessRepoArmLateralProbesKeepTheirOffsetFence pins the three OFFSET 0
// fences (both package_manifest_active arms and the gap CTE). Without the
// fence the planner flattens the LATERAL subquery and starts from the fact
// index again, the pre-#7088 plan (see
// docs/internal/evidence/7088-readiness-repo-scope.md).
func TestReadinessRepoArmLateralProbesKeepTheirOffsetFence(t *testing.T) {
	t.Parallel()

	if got := strings.Count(readinessPackageManifestActiveCTE, repoArmLateralFenceNeedle); got != 2 {
		t.Errorf("package_manifest_active OFFSET 0 fences = %d, want 2 (one per arm)", got)
	}
	if got := strings.Count(repoArmGapCTE(t), repoArmLateralFenceNeedle); got != 1 {
		t.Errorf("package_dependency_gap_active OFFSET 0 fences = %d, want 1", got)
	}
}

// TestReadinessRepoArmManifestArmsStartFromTheRepositoryScopes pins the scope
// anchor in both package_manifest_active arms and rejects the empty-$11 escape
// that forced a per-scope probe of every active scope under a generic plan.
func TestReadinessRepoArmManifestArmsStartFromTheRepositoryScopes(t *testing.T) {
	t.Parallel()

	if got := strings.Count(readinessPackageManifestActiveCTE, repoArmScopeAnchorNeedle); got != 2 {
		t.Errorf("package_manifest_active scope.source_key = $11 anchors = %d, want 2 (one per arm)", got)
	}
	if got := strings.Count(readinessPackageManifestActiveCTE, "AS dependency"); got != 2 {
		t.Errorf("package_manifest_active LATERAL probes = %d, want 2", got)
	}
	for _, escape := range []string{"$11 = '' OR", "$11 = ''"} {
		if strings.Contains(readinessPackageManifestActiveCTE, escape) {
			t.Errorf("package_manifest_active contains the empty-repository escape %q; under a generic plan it probes every active scope", escape)
		}
	}
}

var inListPattern = regexp.MustCompile(`IN \(([^)]*)\)`)

// collapseSQLSpace normalizes whitespace so a query and a migration that
// format the same predicate with different indentation compare equal.
func collapseSQLSpace(sql string) string {
	return strings.Join(strings.Fields(sql), " ")
}

// stripSQLLineComments drops "--" comment lines so prose in a migration header
// cannot satisfy a predicate guard.
func stripSQLLineComments(sql string) string {
	var kept []string
	for _, line := range strings.Split(sql, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// legacyGapIndexMigrationSQL returns the embedded SQL of the migration that
// creates the legacy/gap partial index without its comments, found by file name suffix so a
// renumber does not break the lookup.
func legacyGapIndexMigrationSQL(t *testing.T) string {
	t.Helper()
	for _, def := range migrations.BootstrapDefinitions() {
		if strings.HasSuffix(path.Base(def.Path), "_fact_records_content_entity_dependency_legacy_gap_repo_idx.sql") {
			return stripSQLLineComments(def.SQL)
		}
	}
	t.Fatalf("no embedded migration creates fact_records_content_entity_dependency_legacy_gap_repo_idx")
	return ""
}

// TestReadinessGapAndLegacyReadsStayInsideTheMigrationIndexPredicate binds the
// two readers to the partial-index predicate of the embedded migration. A
// partial index serves a query only when Postgres can prove the query
// predicate implies the index predicate; the readers' predicates and the
// index predicate are separate texts, so this fails when either drifts.
func TestReadinessGapAndLegacyReadsStayInsideTheMigrationIndexPredicate(t *testing.T) {
	t.Parallel()

	migration := collapseSQLSpace(legacyGapIndexMigrationSQL(t))

	gapMatch := inListPattern.FindStringSubmatch(collapseSQLSpace(repoArmGapCTE(t)))
	if gapMatch == nil {
		t.Fatalf("package_dependency_gap_active lost its config_kind IN list")
	}
	migrationMatch := inListPattern.FindStringSubmatch(migration)
	if migrationMatch == nil {
		t.Fatalf("migration lost its config_kind IN list")
	}
	if gapMatch[1] != migrationMatch[1] {
		t.Errorf("gap CTE IN list %q differs from migration index predicate %q", gapMatch[1], migrationMatch[1])
	}
	if !strings.Contains(migration, "(payload->'entity_metadata'->>'config_kind') IN (") {
		t.Errorf("migration index predicate no longer keys the gap kinds on entity_metadata config_kind")
	}

	const legacyPredicate = "payload->>'config_kind' = 'dependency'"
	if !strings.Contains(collapseSQLSpace(readinessPackageManifestActiveCTE), "dependency."+legacyPredicate) {
		t.Errorf("legacy package_manifest_active arm lost %q", legacyPredicate)
	}
	if !strings.Contains(migration, legacyPredicate) {
		t.Errorf("migration index predicate lost %q, so the legacy arm cannot use the index", legacyPredicate)
	}
	for _, shared := range []string{
		"fact_kind = 'content_entity'",
		"source_system = 'git'",
		"is_tombstone = FALSE",
		"payload->>'entity_type' = 'Variable'",
	} {
		if !strings.Contains(migration, shared) {
			t.Errorf("migration index predicate lost %q shared by both readers", shared)
		}
	}
}
