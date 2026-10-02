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

	// Counted on comment-stripped text: a fence commented out with "--" (even at
	// column 0, which keeps the needle's own text intact after the marker) must
	// not count.
	if got := strings.Count(stripSQLLineComments(readinessPackageManifestActiveCTE), repoArmLateralFenceNeedle); got != 2 {
		t.Errorf("package_manifest_active OFFSET 0 fences = %d, want 2 (one per arm)", got)
	}
	if got := strings.Count(stripSQLLineComments(repoArmGapCTE(t)), repoArmLateralFenceNeedle); got != 1 {
		t.Errorf("package_dependency_gap_active OFFSET 0 fences = %d, want 1", got)
	}
}

// TestReadinessRepoArmManifestArmsStartFromTheRepositoryScopes pins the scope
// anchor in both package_manifest_active arms and rejects the empty-$11 escape
// that forced a per-scope probe of every active scope under a generic plan.
func TestReadinessRepoArmManifestArmsStartFromTheRepositoryScopes(t *testing.T) {
	t.Parallel()

	manifest := stripSQLLineComments(readinessPackageManifestActiveCTE)
	if got := strings.Count(manifest, repoArmScopeAnchorNeedle); got != 2 {
		t.Errorf("package_manifest_active scope.source_key = $11 anchors = %d, want 2 (one per arm)", got)
	}
	if got := strings.Count(manifest, "AS dependency"); got != 2 {
		t.Errorf("package_manifest_active LATERAL probes = %d, want 2", got)
	}
	for _, escape := range []string{"$11 = '' OR", "$11 = ''"} {
		if strings.Contains(manifest, escape) {
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

// stripSQLLineComments removes every "--" SQL comment, whole-line or trailing,
// so prose in a migration header or a note after a code line cannot satisfy a
// predicate guard for a condition that was deleted from the code. It cuts at
// the first "--" on a line, which is safe only while no guarded text holds
// "--" inside a quoted literal;
// TestReadinessRepoArmGuardedSQLHasNoDoubleDashInsideLiterals pins that.
func stripSQLLineComments(sql string) string {
	var kept []string
	for _, line := range strings.Split(sql, "\n") {
		if cut := strings.Index(line, "--"); cut >= 0 {
			// Trim the space before the comment so a multi-line needle still
			// matches the code that preceded a trailing comment.
			line = strings.TrimRight(line[:cut], " \t")
			if line == "" {
				continue
			}
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// TestStripSQLLineCommentsDropsEverySQLComment pins the contract the count
// guards rely on: no text after "--" survives, whether the comment fills a
// whole line or trails code. A needle left in a trailing comment must not be
// able to satisfy a guard for a predicate that was deleted from the code.
func TestStripSQLLineCommentsDropsEverySQLComment(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"whole line comment", "A\n  -- B\nC", "A\nC"},
		{"trailing comment", "A -- B\nC", "A\nC"},
		{"trailing comment hides a needle", "WHERE x = 1 -- AND is_tombstone = FALSE", "WHERE x = 1"},
		{"comment at column zero keeps no needle", "--        OFFSET 0\n    ) AS fact", "    ) AS fact"},
		{"no comment", "A\nB", "A\nB"},
		{"operators that are not comments", "a->>'k' = 'v' AND b - c > 0", "a->>'k' = 'v' AND b - c > 0"},
	}
	for _, tc := range cases {
		if got := stripSQLLineComments(tc.in); got != tc.want {
			t.Errorf("%s: stripSQLLineComments(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

// TestReadinessRepoArmGuardedSQLHasNoDoubleDashInsideLiterals proves the
// comment cut is safe on the guarded text: no guarded constant carries "--"
// inside a quoted literal, where cutting at "--" would corrupt real SQL.
func TestReadinessRepoArmGuardedSQLHasNoDoubleDashInsideLiterals(t *testing.T) {
	t.Parallel()

	for name, sql := range map[string]string{
		"package_manifest_active":        readinessPackageManifestActiveCTE,
		"package_dependency_gap_active":  repoArmGapCTE(t),
		"migration 159 index definition": legacyGapIndexMigrationSQLWithComments(t),
	} {
		for _, line := range strings.Split(sql, "\n") {
			cut := strings.Index(line, "--")
			if cut < 0 {
				continue
			}
			// A "--" outside a literal has an even number of single quotes before
			// it. Apostrophes inside the comment text that follows do not count.
			if strings.Count(line[:cut], "'")%2 == 1 {
				t.Errorf("%s: %q has a %q inside a quoted literal; the comment cut would corrupt it", name, line, "--")
			}
		}
	}
}

// TestReadinessRepoArmGuardedSQLHasNoBlockComments pins that the guarded text
// holds no "/* */" block comment. stripSQLLineComments removes only "--"
// comments, so a block comment could still carry a needle for a predicate that
// was deleted from the code. The shipped SQL has none; adding one must be a
// deliberate change to this guard, not a way around it.
func TestReadinessRepoArmGuardedSQLHasNoBlockComments(t *testing.T) {
	t.Parallel()

	for name, sql := range map[string]string{
		"package_manifest_active":        readinessPackageManifestActiveCTE,
		"package_dependency_gap_active":  repoArmGapCTE(t),
		"migration 159 index definition": legacyGapIndexMigrationSQLWithComments(t),
	} {
		if strings.Contains(sql, "/*") || strings.Contains(sql, "*/") {
			t.Errorf("%s contains a block comment; the guards strip only -- comments", name)
		}
	}
}

// legacyGapIndexMigrationSQLWithComments returns the embedded migration text
// unchanged, for scans that must see the raw bytes.
func legacyGapIndexMigrationSQLWithComments(t *testing.T) string {
	t.Helper()
	for _, def := range migrations.BootstrapDefinitions() {
		if strings.HasSuffix(path.Base(def.Path), "_fact_records_content_entity_dependency_legacy_gap_repo_idx.sql") {
			return def.SQL
		}
	}
	t.Fatalf("no embedded migration creates fact_records_content_entity_dependency_legacy_gap_repo_idx")
	return ""
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

const (
	// repoArmGenerationBindNeedle binds a LATERAL probe to the scope's active
	// generation. It is code-shaped: no SQL comment spells it this way.
	repoArmGenerationBindNeedle = "dependency.generation_id = scope.active_generation_id"
	// repoArmTombstoneNeedle keeps tombstoned facts out of a probe.
	repoArmTombstoneNeedle = "dependency.is_tombstone = FALSE"
	// repoArmCurrentArmExclusivityNeedle is arm 1's mutual exclusion with the
	// legacy arm: a row with a top-level config_kind belongs to arm 2 only.
	repoArmCurrentArmExclusivityNeedle = "AND NULLIF(dependency.payload->>'config_kind', '') IS NULL"
	// repoArmLegacyArmNeedle is arm 2's legacy payload shape (#7301).
	repoArmLegacyArmNeedle = "AND dependency.payload->>'config_kind' = 'dependency'"
	// repoArmRepositoryBindNeedle binds a probe to the anchored repository. The
	// "dependency." alias keeps the outer consumers' own repo_id filters, which
	// are not probes, from satisfying the count.
	repoArmRepositoryBindNeedle = "dependency.payload->>'repo_id' = $11"
)

// TestReadinessRepoArmProbesBindGenerationAndTombstone pins, from the shipped
// constants, that all three LATERAL probes (both package_manifest_active arms
// and the gap CTE) read only the scope's active generation and skip tombstones.
// Dropping either from one probe changes the answer (stale generations or
// deleted facts leak in) while the plan still looks fine.
func TestReadinessRepoArmProbesBindGenerationAndTombstone(t *testing.T) {
	t.Parallel()

	probes := collapseProbeText(readinessPackageManifestActiveCTE) + " " + collapseProbeText(repoArmGapCTE(t))
	if got := strings.Count(probes, repoArmGenerationBindNeedle); got != 3 {
		t.Errorf("%q occurrences = %d, want 3 (two manifest arms + gap CTE)", repoArmGenerationBindNeedle, got)
	}
	if got := strings.Count(probes, repoArmTombstoneNeedle); got != 3 {
		t.Errorf("%q occurrences = %d, want 3 (two manifest arms + gap CTE)", repoArmTombstoneNeedle, got)
	}
}

// TestReadinessRepoArmManifestArmsStayMutuallyExclusive pins that each payload
// shape is read by exactly one arm: arm 1 requires an absent top-level
// config_kind and arm 2 requires the legacy value. Without the exclusivity a
// row that carries both shapes would be counted twice by the UNION ALL.
func TestReadinessRepoArmManifestArmsStayMutuallyExclusive(t *testing.T) {
	t.Parallel()

	manifest := collapseProbeText(readinessPackageManifestActiveCTE)
	if got := strings.Count(manifest, repoArmCurrentArmExclusivityNeedle); got != 1 {
		t.Errorf("arm 1 exclusivity %q occurrences = %d, want exactly 1", repoArmCurrentArmExclusivityNeedle, got)
	}
	if got := strings.Count(manifest, repoArmLegacyArmNeedle); got != 1 {
		t.Errorf("arm 2 legacy predicate %q occurrences = %d, want exactly 1", repoArmLegacyArmNeedle, got)
	}
}

// TestReadinessRepoArmProbesBindTheAnchoredRepository pins that all three
// LATERAL probes filter on the anchored repository's id. repo_id is the leading
// key of the indexes the probes use (migrations 121 and 159) and the Index Cond
// the live proof asserts; a probe without it reads every fact in the scope
// instead of the repository's own, the pre-#7088 scan shape, while the scope
// and generation conditions still look right.
func TestReadinessRepoArmProbesBindTheAnchoredRepository(t *testing.T) {
	t.Parallel()

	if got := strings.Count(collapseProbeText(readinessPackageManifestActiveCTE), repoArmRepositoryBindNeedle); got != 2 {
		t.Errorf("package_manifest_active %q occurrences = %d, want 2 (one per arm)", repoArmRepositoryBindNeedle, got)
	}
	if got := strings.Count(collapseProbeText(repoArmGapCTE(t)), repoArmRepositoryBindNeedle); got != 1 {
		t.Errorf("package_dependency_gap_active %q occurrences = %d, want 1", repoArmRepositoryBindNeedle, got)
	}
}

// collapseProbeText drops SQL line comments and normalizes whitespace so a
// needle is matched against executable SQL only.
func collapseProbeText(sql string) string {
	return collapseSQLSpace(stripSQLLineComments(sql))
}

// TestReadinessEmptyRepositoryAlwaysCarriesATargetAnchor pins the implication
// that makes the removed empty-$11 escape unreachable: every query that has a
// fact anchor but no repository id has needsTargetResolution() true, so the
// production argument builder sets $20 and package_manifest_dependency takes
// the consumption-key arm instead of reading package_manifest_active. A
// repository-only anchor takes the manifest arm ($20 = false) with a non-empty
// $11. The cases call the production methods, not copies.
func TestReadinessEmptyRepositoryAlwaysCarriesATargetAnchor(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		query        ReadinessQuery
		wantResolved bool
	}{
		{name: "cve only", query: ReadinessQuery{CVEID: "CVE-2024-0001"}, wantResolved: true},
		{name: "package only", query: ReadinessQuery{PackageID: "pkg:npm/left-pad"}, wantResolved: true},
		{name: "subject digest only", query: ReadinessQuery{SubjectDigest: "sha256:abc"}, wantResolved: true},
		{name: "image ref only", query: ReadinessQuery{ImageRef: "registry.example/app:1"}, wantResolved: true},
		{name: "repository only", query: ReadinessQuery{RepositoryID: "repository:r_1"}, wantResolved: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if !tc.query.hasFactAnchor() {
				t.Fatalf("hasFactAnchor() = false, want true: the store would return before reading")
			}
			if got := tc.query.needsTargetResolution(); got != tc.wantResolved {
				t.Fatalf("needsTargetResolution() = %t, want %t", got, tc.wantResolved)
			}
			_, _, _, resolved := readinessTargetArguments(readinessTarget{}, tc.query)
			if resolved != tc.wantResolved {
				t.Fatalf("readinessTargetArguments $20 = %t, want %t", resolved, tc.wantResolved)
			}
			args := readinessArgsForQuery(tc.query)
			if got := args[19]; got != tc.wantResolved {
				t.Fatalf("bound $20 = %v, want %t", got, tc.wantResolved)
			}
			if strings.TrimSpace(tc.query.RepositoryID) == "" && !resolved {
				t.Fatalf("empty repository id with $20 = false would read package_manifest_active unanchored")
			}
		})
	}
}
