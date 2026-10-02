// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// Helpers for the #7088 repository-arm proof in
// readiness_package_manifest_repo_scope_live_test.go. They live in a plain
// _test.go file so the live-test ledger keeps one row for the live proof.

const (
	repoArmTargetRepoID  = "repository:r_repo_arm_target"
	repoArmRefOnlyRepoID = "repository:r_repo_arm_ref_only"
	repoArmUnknownRepoID = "repository:r_repo_arm_unknown"
	repoArmNoiseScopes   = 650
	// repoArmTargetScopes is the number of scopes (repository plus
	// repository_ref) the target repository owns; every per-scope probe the
	// readiness statement makes for it must be bounded by this count.
	repoArmTargetScopes = 2

	repoArmDependencyIndex   = "fact_records_content_entity_dependency_variable_repo_idx"
	repoArmLegacyAndGapIndex = "fact_records_content_entity_dependency_legacy_gap_repo_idx"
	// repoArmStatementBufferLimit bounds the whole readiness statement for the
	// target repository. The fixed cost of the other CTEs on this corpus is
	// ~7.5k shared buffers; the pre-#7088 whole statement read 238,642 on it
	// (custom plan; evidence table in
	// docs/internal/evidence/7088-readiness-repo-scope.md), mostly arm 2
	// probing ~300 content_entity rows per noise scope, and arm 1 without the
	// generation in its Index Cond read ~30k on the target's old generations.
	repoArmStatementBufferLimit = 20000
	// repoArmDependencyScanBufferLimit bounds the shared buffers of the three
	// dependency-variable reads themselves, under custom and generic plans.
	// Bounded, they read 588 buffers on this corpus (evidence table in
	// docs/internal/evidence/7088-readiness-repo-scope.md); the pre-#7088
	// dependency-variable reads read 211,558 (custom) and 204,290 (generic).
	repoArmDependencyScanBufferLimit = 2000
)

// previousRepoArmManifestCTE and previousRepoArmGapCTE are the
// package_manifest_active and package_dependency_gap_active texts as they
// stood before #7088 (comments removed). The differential test splices them
// back into the shipped ListReadinessQuery, so everything except the two
// rewritten CTEs is derived from production and cannot drift.
const previousRepoArmManifestCTE = `package_manifest_active AS (
    SELECT fact.fact_id, fact.payload, fact.observed_at
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = 'content_entity'
      AND fact.source_system = 'git'
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
      AND fact.payload->>'entity_type' = 'Variable'
      AND NULLIF(fact.payload->>'config_kind', '') IS NULL
      AND fact.payload->'entity_metadata'->>'config_kind' = 'dependency'
      AND ($11 = '' OR fact.payload->>'repo_id' = $11)
    UNION ALL
    SELECT fact.fact_id, fact.payload, fact.observed_at
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = 'content_entity'
      AND fact.source_system = 'git'
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
      AND fact.payload->>'entity_type' = 'Variable'
      AND fact.payload->>'config_kind' = 'dependency'
      AND ($11 = '' OR fact.payload->>'repo_id' = $11)
),
`

const previousRepoArmGapCTE = `package_dependency_gap_active AS (
    SELECT fact.payload, fact.observed_at
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = 'content_entity'
      AND fact.source_system = 'git'
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
      AND fact.payload->>'entity_type' = 'Variable'
      AND fact.payload->'entity_metadata'->>'config_kind' IN (
          'vcs_dependency',
          'path_dependency',
          'url_dependency',
          'editable_dependency',
          'unsupported_dependency'
      )
      AND $11 <> ''
      AND scope.source_key = $11
      AND fact.payload->>'repo_id' = $11
),
`

// previousRepoArmReadinessQuery rebuilds the pre-#7088 readiness statement by
// splicing the previous CTE texts over the current ones.
func previousRepoArmReadinessQuery(t *testing.T) string {
	t.Helper()
	query := spliceRepoArmCTE(t, ListReadinessQuery, "package_manifest_active AS (", "package_registry_active AS (", previousRepoArmManifestCTE)
	return spliceRepoArmCTE(t, query, "package_dependency_gap_active AS (", "unsupported_target_rows AS (", previousRepoArmGapCTE)
}

func spliceRepoArmCTE(t *testing.T, query, start, end, replacement string) string {
	t.Helper()
	i := strings.Index(query, start)
	j := strings.Index(query, end)
	if i < 0 || j < i || strings.Count(query, start) != 1 {
		t.Fatalf("splice markers %q..%q not found exactly once in readiness query", start, end)
	}
	return query[:i] + replacement + query[j:]
}

// TestPreviousRepoArmReadinessQuerySplicesOnlyTheTwoCTEs keeps the
// differential honest without a database: the spliced statement differs from
// production only inside the two rewritten CTEs.
func TestPreviousRepoArmReadinessQuerySplicesOnlyTheTwoCTEs(t *testing.T) {
	previous := previousRepoArmReadinessQuery(t)
	if !strings.Contains(previous, previousRepoArmManifestCTE) || !strings.Contains(previous, previousRepoArmGapCTE) {
		t.Fatal("previous readiness query is missing a spliced CTE")
	}
	prefix, _, foundPrefix := strings.Cut(ListReadinessQuery, "package_manifest_active AS (")
	_, rest, foundSuffix := strings.Cut(ListReadinessQuery, "unsupported_target_rows AS (")
	if !foundPrefix || !foundSuffix {
		t.Fatal("readiness query is missing a splice marker")
	}
	suffix := "unsupported_target_rows AS (" + rest
	if !strings.HasPrefix(previous, prefix) || !strings.HasSuffix(previous, suffix) {
		t.Fatal("previous readiness query diverges from production outside the rewritten CTEs")
	}
}

// readinessArgsForQuery returns the 20 arguments readSupplyChainImpactReadiness
// binds for query, in the same order, with the target-resolution arguments
// ($17-$20) taken from readinessTargetArguments for a target with no resolved
// package keys. It differs from production in one way: $17-$19 go through
// nonNilStrings, so an empty target renders as an empty array instead of NULL
// in the SQL-level prepared statements these proofs use; $20 is the production
// value. The statement takes exactly 20 arguments; a shorter list fails with
// "expected 20 arguments".
func readinessArgsForQuery(query ReadinessQuery) []any {
	ecosystems, packageNames, packageIDs, resolved := readinessTargetArguments(readinessTarget{}, query)
	return []any{
		array.Of(vulnerabilityAdvisoryFactKinds),
		array.Of(vulnerabilityExploitabilityFactKinds),
		array.Of(packageConsumptionCorrelationFactKinds),
		array.Of(packageRegistryFactKinds),
		array.Of(sbomComponentFactKinds),
		array.Of(sbomAttestationFactKinds),
		array.Of(containerImageIdentityFactKinds),
		array.Of(vulnerabilitySourceSnapshotFactKinds),
		query.CVEID, query.PackageID, query.RepositoryID, query.SubjectDigest, query.AdvisoryID, query.ImageRef,
		array.Of(vulnerabilityOSPackageFactKinds),
		array.Of(scannerWorkerAnalysisFactKinds),
		array.Of(nonNilStrings(ecosystems)),
		array.Of(nonNilStrings(packageNames)),
		array.Of(nonNilStrings(packageIDs)),
		resolved,
	}
}

// nonNilStrings turns a nil slice into an empty one so the SQL-level prepared
// statement path renders an empty array literal instead of NULL.
func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// readinessArgsForRepository returns the production arguments for a
// repository-only anchor ($20 = false: no CVE/package/digest/image target
// resolution).
func readinessArgsForRepository(repositoryID string) []any {
	return readinessArgsForQuery(ReadinessQuery{RepositoryID: repositoryID})
}

type repoArmQueryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// readinessResultRows renders every result row as one comparable string,
// sorted, so two statements can be compared row for row.
func readinessResultRows(t *testing.T, ctx context.Context, db repoArmQueryer, query string, args ...any) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("query readiness rows: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var family sql.NullString
		var factCount sql.NullInt64
		var latest sql.NullTime
		var incomplete sql.NullBool
		var reasons array.StringArray
		var snapshots, states, unsupported sql.NullString
		if err := rows.Scan(&family, &factCount, &latest, &incomplete, &reasons, &snapshots, &states, &unsupported); err != nil {
			t.Fatalf("scan readiness row: %v", err)
		}
		out = append(out, fmt.Sprintf("%v|%v|%v|%v|%v|%v|%v|%v", family, factCount, latest, incomplete, reasons, snapshots, states, unsupported))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("readiness rows: %v", err)
	}
	sort.Strings(out)
	return out
}

// repoArmPlanNode is the subset of an EXPLAIN (FORMAT JSON) node the plan
// assertions read.
type repoArmPlanNode struct {
	NodeType     string            `json:"Node Type"`
	RelationName string            `json:"Relation Name"`
	IndexName    string            `json:"Index Name"`
	IndexCond    string            `json:"Index Cond"`
	Filter       string            `json:"Filter"`
	ActualLoops  float64           `json:"Actual Loops"`
	SharedHit    int64             `json:"Shared Hit Blocks"`
	SharedRead   int64             `json:"Shared Read Blocks"`
	Plans        []repoArmPlanNode `json:"Plans"`
}

// decodeRepoArmPlan returns the root plan node of one EXPLAIN (ANALYZE,
// BUFFERS, FORMAT JSON) document.
func decodeRepoArmPlan(t *testing.T, raw []byte) repoArmPlanNode {
	t.Helper()
	var plans []struct {
		Plan repoArmPlanNode `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &plans); err != nil || len(plans) != 1 {
		t.Fatalf("decode readiness plan: count=%d err=%v", len(plans), err)
	}
	return plans[0].Plan
}

// repoArmDependencyScans returns every fact_records scan that reads
// dependency-variable rows: package_manifest_active's two arms and
// package_dependency_gap_active. They are the only fact_records reads in the
// statement whose predicate names entity_type 'Variable', config_kind or
// repo_id, or that use one of the two dependency-variable partial indexes
// (whose predicate carries them).
func repoArmDependencyScans(node repoArmPlanNode) []repoArmPlanNode {
	var out []repoArmPlanNode
	if node.RelationName == "fact_records" &&
		(strings.Contains(node.IndexCond+node.Filter, "'Variable'") ||
			strings.Contains(node.IndexCond+node.Filter, "'config_kind'") ||
			strings.Contains(node.IndexCond+node.Filter, "'repo_id'") ||
			node.IndexName == repoArmDependencyIndex || node.IndexName == repoArmLegacyAndGapIndex) {
		out = append(out, node)
	}
	for _, child := range node.Plans {
		out = append(out, repoArmDependencyScans(child)...)
	}
	return out
}

// assertRepoArmPlanBounded fails unless every dependency-variable read is an
// index probe through one of the two repo-leading partial indexes with the
// scope and active generation in its Index Cond, executed at most once per
// target scope, and their buffers stay under repoArmDependencyScanBufferLimit.
// A positive statementBufferLimit also bounds the whole statement; the
// generic-plan proof passes 0 because other, fact-kind-anchored CTEs
// (advisory, package consumption) probe every active scope under a generic
// plan on this corpus, which #7088 does not change.
func assertRepoArmPlanBounded(t *testing.T, label string, raw []byte, statementBufferLimit int64) {
	t.Helper()
	root := decodeRepoArmPlan(t, raw)
	scans := repoArmDependencyScans(root)
	used := map[string]int{}
	var scanBuffers int64
	for _, scan := range scans {
		used[scan.IndexName]++
		scanBuffers += scan.SharedHit + scan.SharedRead
		if scan.IndexName != repoArmDependencyIndex && scan.IndexName != repoArmLegacyAndGapIndex {
			t.Errorf("%s: dependency-variable read uses %s %q (loops=%.0f), want %s or %s",
				label, scan.NodeType, scan.IndexName, scan.ActualLoops, repoArmDependencyIndex, repoArmLegacyAndGapIndex)
		}
		if !strings.Contains(scan.IndexCond, "scope_id") || !strings.Contains(scan.IndexCond, "generation_id") {
			t.Errorf("%s: %s Index Cond %q does not bound scope_id and generation_id (active generation not index-served)",
				label, scan.IndexName, scan.IndexCond)
		}
		if scan.ActualLoops > repoArmTargetScopes {
			t.Errorf("%s: %s %q ran %.0f loops, want <= %d (one probe per target scope, not per active scope)",
				label, scan.NodeType, scan.IndexName, scan.ActualLoops, repoArmTargetScopes)
		}
	}
	if used[repoArmDependencyIndex] < 1 || used[repoArmLegacyAndGapIndex] < 2 {
		t.Errorf("%s: dependency index usage = %v, want arm 1 on %s and arm 2 plus the gap CTE on %s",
			label, used, repoArmDependencyIndex, repoArmLegacyAndGapIndex)
	}
	if scanBuffers > repoArmDependencyScanBufferLimit {
		t.Errorf("%s: dependency-variable reads used %d shared buffers, want <= %d", label, scanBuffers, repoArmDependencyScanBufferLimit)
	}
	if buffers := root.SharedHit + root.SharedRead; statementBufferLimit > 0 && buffers > statementBufferLimit {
		t.Errorf("%s: statement shared buffers = %d, want <= %d", label, buffers, statementBufferLimit)
	}
	t.Logf("%s: dependency scans=%d index usage=%v dependency-scan buffers=%d statement shared buffers=%d",
		label, len(scans), used, scanBuffers, root.SharedHit+root.SharedRead)
}

// repoArmExecuteStatement renders an EXECUTE of the SQL-level prepared
// statement with literal arguments: the server reports no bind parameters
// for EXECUTE, so they cannot be sent as $n. Inputs are test constants.
func repoArmExecuteStatement(t *testing.T, name string, args []any) string {
	t.Helper()
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	literals := make([]string, 0, len(args))
	for _, arg := range args {
		switch v := arg.(type) {
		case string:
			literals = append(literals, quote(v))
		case bool:
			literals = append(literals, fmt.Sprintf("%t", v))
		case driver.Valuer:
			value, err := v.Value()
			if err != nil {
				t.Fatalf("render array argument: %v", err)
			}
			text, ok := value.(string)
			if !ok {
				t.Fatalf("array argument rendered as %T, want string", value)
			}
			literals = append(literals, quote(text))
		default:
			t.Fatalf("unsupported EXECUTE argument %T", arg)
		}
	}
	return "EXECUTE " + name + "(" + strings.Join(literals, ", ") + ")"
}
