// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// preHoistListDeferredScopedRelationshipFactRecordsQuery is a frozen copy of the
// query shape BEFORE the #3624 payload-hoist rewrite (2026-07 pg_stat_statements
// evidence: 4,647s/907 calls). It exists ONLY in this test file so the
// differential proof below can compare identical params against the OLD SQL
// shape and the NEW hoisted shape and assert the returned fact_id sets are
// exactly equal. Production code must never re-introduce this shape; if this
// constant and listDeferredScopedRelationshipFactRecordsQuery ever need to
// diverge for a reason other than proving equivalence, delete this test.
const preHoistListDeferredScopedRelationshipFactRecordsQuery = latestGenerationCTE + `,
matched_facts AS MATERIALIZED (
    SELECT
        fact.fact_id,
        fact.scope_id,
        fact.generation_id,
        fact.fact_kind,
        fact.stable_fact_key,
        fact.schema_version,
        fact.collector_kind,
        fact.fencing_token,
        fact.source_confidence,
        fact.source_system,
        fact.source_fact_key,
        COALESCE(fact.source_uri, '') AS source_uri,
        COALESCE(fact.source_record_id, '') AS source_record_id,
        fact.observed_at,
        fact.is_tombstone,
        fact.payload
    FROM fact_records AS fact
    WHERE fact.scope_id = $3
      AND fact.generation_id = $4
      AND fact.fact_kind IN ('content', 'file', 'gcp_cloud_relationship')
      AND (
        lower(fact.payload::text) LIKE ANY($1)
        OR EXISTS (
          SELECT 1
          FROM unnest($2::text[]) AS catalog_repo_id(value)
          WHERE catalog_repo_id.value <> lower(COALESCE(fact.payload->>'repo_id', ''))
            AND lower(fact.payload::text) LIKE
              '%' ||
              replace(replace(replace(catalog_repo_id.value, '\', '\\'), '%', '\%'), '_', '\_') ||
              '%' ESCAPE '\'
        )
      )
)
SELECT
    fact.fact_id,
    fact.scope_id,
    fact.generation_id,
    fact.fact_kind,
    fact.stable_fact_key,
    fact.schema_version,
    fact.collector_kind,
    fact.fencing_token,
    fact.source_confidence,
    fact.source_system,
    fact.source_fact_key,
    fact.source_uri,
    fact.source_record_id,
    fact.observed_at,
    fact.is_tombstone,
    fact.payload
FROM matched_facts AS fact
JOIN latest_generations AS latest
  ON latest.scope_id = fact.scope_id
 AND latest.generation_id = fact.generation_id
WHERE latest.generation_id IS NOT NULL
ORDER BY fact.observed_at ASC, fact.fact_id ASC
`

// dsnForDeferredHoistProof returns the proof DSN or skips the test. It reuses
// the shared latest-generation proof DSN when the dedicated one is unset, same
// convention as ingestion_backfill_partition_integration_test.go.
func dsnForDeferredHoistProof(t *testing.T) string {
	t.Helper()
	if dsn := os.Getenv("ESHU_DEFERRED_HOIST_PROOF_DSN"); dsn != "" {
		return dsn
	}
	if dsn := os.Getenv("ESHU_DEFERRED_PARTITION_PROOF_DSN"); dsn != "" {
		return dsn
	}
	if dsn := os.Getenv("ESHU_LATEST_GENERATION_PROOF_DSN"); dsn != "" {
		return dsn
	}
	t.Skip("set ESHU_DEFERRED_HOIST_PROOF_DSN (or ESHU_DEFERRED_PARTITION_PROOF_DSN / ESHU_LATEST_GENERATION_PROOF_DSN) to run the deferred fact-load hoist Postgres differential proof")
	return ""
}

func openDeferredHoistProofDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func provisionDeferredHoistSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	schemaName := fmt.Sprintf("deferred_hoist_proof_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create proof schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName+", public"); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	if _, err := db.ExecContext(ctx, deferredPartitionProofSchemaSQL); err != nil {
		t.Fatalf("create proof tables: %v", err)
	}
}

// deferredHoistFixtureRow is one seeded fact_records row for the differential
// proof, covering the carve-outs the #3624 fix must preserve exactly.
type deferredHoistFixtureRow struct {
	factID  string
	scopeID string
	genID   string
	kind    string
	payload string
}

// seedDeferredHoistFixture seeds two partitions covering every carve-out named
// in the fix's acceptance criteria:
//
//   - "git-repository-scope:github.com/org/app" (own repo_id "github.com/org/app",
//     the scope_id-derived $6 performance hint the fast arm expects to match):
//     app vs app-config prefix-overlap cross-repo reference (own matches $6, fast
//     arm engages); own-repo self-mention only (must be excluded); an ArgoCD
//     marker fact (unconditional $1 over-select); a no-match noise row; and a
//     "$6 mismatch" row whose OWN repo_id ("repo-orphan") differs from this
//     partition's derived $6, so it exercises the fallback arm even though it
//     lives in a $6-hinted partition, proving the fallback still fires per-row
//     when $6 is wrong for that specific row.
//   - "gcp:project:hoist:relationship:global" (not a git-repository-scope shape,
//     so $6 derives to the empty string): a cloud-relationship fact with an
//     empty repo_id (own_repo_id equals the empty string, matching $6, so the
//     fast arm engages for the empty-own case) referencing another catalog
//     repo_id verbatim.
func seedDeferredHoistFixture(t *testing.T, ctx context.Context, db *sql.DB) []deferredHoistFixtureRow {
	t.Helper()
	base := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

	const gitScope = "git-repository-scope:github.com/org/app"
	const gcpScope = "gcp:project:hoist:relationship:global"

	scopes := []string{gitScope, gcpScope}
	for _, scopeID := range scopes {
		if _, err := db.ExecContext(ctx,
			"INSERT INTO ingestion_scopes (scope_id, active_generation_id) VALUES ($1, NULL)", scopeID); err != nil {
			t.Fatalf("seed scope %q: %v", scopeID, err)
		}
	}
	gens := map[string]string{
		gitScope: "gen-hoist",
		gcpScope: "gen-gcp-hoist",
	}
	for scopeID, genID := range gens {
		if _, err := db.ExecContext(ctx,
			"INSERT INTO scope_generations (generation_id, scope_id, ingested_at) VALUES ($1, $2, $3)",
			genID, scopeID, base); err != nil {
			t.Fatalf("seed generation %q: %v", genID, err)
		}
	}

	rows := []deferredHoistFixtureRow{
		{
			// app vs app-config prefix overlap: source references the target's FULL
			// repo_id verbatim, must survive self-exclusion. own_repo_id here equals
			// this partition's derived $6 ("github.com/org/app"), so this row is
			// resolved by the FAST arm ($5 excludes $6, still contains app-config).
			factID:  "fact-app-refs-app-config",
			scopeID: gitScope,
			genID:   "gen-hoist",
			kind:    "content",
			payload: `{"repo_id":"github.com/org/app","artifact_type":"terraform","relative_path":"main.tf","content":"app_repo = \"github.com/org/app-config\""}`,
		},
		{
			// own-repo self-mention only: must be excluded by both old and new SQL,
			// the in-memory matcher would drop it anyway. own_repo_id = $6, so this
			// is also resolved by the fast arm ($5 excludes exactly this value).
			factID:  "fact-self-mention-only",
			scopeID: gitScope,
			genID:   "gen-hoist",
			kind:    "content",
			payload: `{"repo_id":"github.com/org/app","artifact_type":"terraform","relative_path":"main.tf","content":"source = \"github.com/org/app//modules/local\""}`,
		},
		{
			// ArgoCD marker fact: unconditionally over-selected via $1 regardless of
			// repo_id content, must survive in both forms.
			factID:  "fact-argocd-application",
			scopeID: gitScope,
			genID:   "gen-hoist",
			kind:    "content",
			payload: `{"repo_id":"github.com/org/app","artifact_type":"argocd","relative_path":"app.yaml","content":"kind: Application\nspec:\n  source:\n    repoURL: https://example.invalid/org/payments-service.git\n"}`,
		},
		{
			// cloud-scope fact with EMPTY repo_id: own_repo_id resolves to the empty
			// string via the COALESCE, and this partition's derived $6 is also the
			// empty string (not a git-repository-scope shape), so the FAST arm
			// engages for the empty-own case; it references another catalog repo_id
			// verbatim.
			factID:  "fact-gcp-empty-repo-id",
			scopeID: gcpScope,
			genID:   "gen-gcp-hoist",
			kind:    "gcp_cloud_relationship",
			payload: `{"source_full_resource_name":"//run.googleapis.com/projects/hoist/locations/us-central1/services/order-gateway","source_asset_type":"run.googleapis.com/Service","relationship_type":"run_service_uses_secret","target_full_resource_name":"//secretmanager.googleapis.com/projects/hoist/secrets/payments-service","target_asset_type":"secretmanager.googleapis.com/Secret","support_state":"supported"}`,
		},
		{
			// $6-MISMATCH row: this fact lives in the git-repository-scope partition
			// whose derived $6 is "github.com/org/app", but its OWN repo_id
			// ("repo-orphan") is different (own_repo_id <> $6). It is not a catalog
			// member at all, but it references a real catalog repo_id verbatim, so
			// it exercises the FALLBACK EXISTS arm even inside a $6-hinted partition
			// — proving the fallback still resolves a row correctly when $6 is wrong
			// for that specific row.
			factID:  "fact-orphan-repo-references-catalog",
			scopeID: gitScope,
			genID:   "gen-hoist",
			kind:    "file",
			payload: `{"repo_id":"repo-orphan","artifact_type":"github_actions_workflow","relative_path":".github/workflows/ci.yaml","content":"jobs:\n  build:\n    uses: org/deploy-toolkit/.github/workflows/deploy.yaml@main\n"}`,
		},
		{
			// no-match noise row: references nothing in the catalog, must be excluded
			// by both forms. own_repo_id = $6, resolved by the fast arm.
			factID:  "fact-no-match-noise",
			scopeID: gitScope,
			genID:   "gen-hoist",
			kind:    "content",
			payload: `{"repo_id":"github.com/org/app","artifact_type":"terraform","relative_path":"main.tf","content":"locals { setting = \"value\" }"}`,
		},
	}

	for _, row := range rows {
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, $4, $5, 'git', $5, $6, $6, $7::jsonb)`,
			row.factID, row.scopeID, row.genID, row.kind, row.factID, base, row.payload); err != nil {
			t.Fatalf("seed fact %q: %v", row.factID, err)
		}
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO relationship_reference_candidate_keys (
    fact_id, scope_id, generation_id, source_repo_id, reference_key
)
SELECT
    fact_id,
    scope_id,
    generation_id,
    COALESCE(NULLIF(LOWER(TRIM(payload->>'repo_id')), ''), ''),
    '|' ||
    REGEXP_REPLACE(
        REGEXP_REPLACE(LOWER(payload::text), '\.git', '', 'g'),
        '[^a-z0-9._-]+',
        '|',
        'g'
    ) ||
    '|'
FROM fact_records
WHERE fact_kind IN ('content', 'file', 'gcp_cloud_relationship')
  AND is_tombstone = FALSE`); err != nil {
		t.Fatalf("seed relationship reference keys: %v", err)
	}
	return rows
}

// deferredHoistCatalog is the full catalog the differential proof discovers
// evidence against; it must include every repo_id referenced by the fixture
// above (including targets that never appear as a fact source themselves).
func deferredHoistCatalog() []relationships.CatalogEntry {
	return []relationships.CatalogEntry{
		{RepoID: "github.com/org/app", Aliases: []string{"github.com/org/app", "edge-app"}},
		{RepoID: "github.com/org/app-config", Aliases: []string{"github.com/org/app-config"}},
		{RepoID: "repo-vault", Aliases: []string{"repo-vault", "secret-store"}},
		{RepoID: "repo-gitops", Aliases: []string{"repo-gitops", "gitops-controller"}},
		{RepoID: "repo-payments", Aliases: []string{"repo-payments", "payments-service"}},
		{RepoID: "repo-gcp-source", Aliases: []string{"repo-gcp-source", "order-gateway"}},
		{RepoID: "deploy-toolkit", Aliases: []string{"deploy-toolkit"}},
		{RepoID: "repo-noise-target", Aliases: []string{"repo-noise-target"}},
	}
}

// runPreHoistDeferredScopedQuery executes the frozen pre-hoist ($1..$4) query
// shape and returns the loaded fact_id set.
func runPreHoistDeferredScopedQuery(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	params deferredScopedFactQueryParams,
	scopeID, generationID string,
) []facts.Envelope {
	t.Helper()
	rows, err := db.QueryContext(
		ctx,
		preHoistListDeferredScopedRelationshipFactRecordsQuery,
		params.nonRepoIDLike, params.repoIDValues, scopeID, generationID,
	)
	if err != nil {
		t.Fatalf("query (pre-hoist): %v", err)
	}
	return collectDeferredScopedRows(t, rows)
}

// runHoistedDeferredScopedQuery executes the production ($1..$7) hoisted query
// shape — building $5/$6 exactly as loadDeferredScopedRelationshipFactsForPartition
// does — and returns the loaded fact_id set.
func runHoistedDeferredScopedQuery(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	params deferredScopedFactQueryParams,
	scopeID, generationID string,
) []facts.Envelope {
	t.Helper()
	ownRepoID := deferredScopedFactOwnRepoIDFromScope(scopeID)
	regex, ok := buildDeferredRepoIDRegex([]string(params.repoIDValues), ownRepoID)
	repoIDReferenceKeys := deferredRepoIDReferenceKeys(params.repoIDValues, params.repoIDReferenceKey)
	var regexParam sql.NullString
	if ok {
		regexParam = sql.NullString{String: regex, Valid: true}
	}
	rows, err := db.QueryContext(
		ctx,
		listDeferredScopedRelationshipFactRecordsQuery,
		params.nonRepoIDLike, params.repoIDValues, scopeID, generationID, regexParam, ownRepoID, repoIDReferenceKeys,
	)
	if err != nil {
		t.Fatalf("query (hoisted): %v", err)
	}
	return collectDeferredScopedRows(t, rows)
}

func collectDeferredScopedRows(t *testing.T, rows *sql.Rows) []facts.Envelope {
	t.Helper()
	defer func() { _ = rows.Close() }()

	var loaded []facts.Envelope
	for rows.Next() {
		envelope, scanErr := scanFactEnvelope(rows)
		if scanErr != nil {
			t.Fatalf("scan: %v", scanErr)
		}
		loaded = append(loaded, envelope)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}
	return loaded
}

func factIDSet(envelopes []facts.Envelope) []string {
	ids := make([]string, 0, len(envelopes))
	for _, e := range envelopes {
		ids = append(ids, e.FactID)
	}
	sort.Strings(ids)
	return ids
}

// TestDeferredScopedFactLoadHoistExactlyEquivalentToPreHoistShape is the #3624
// differential proof. It runs the OLD (pre-hoist) query shape and the NEW
// (payload-hoisted) query shape — listDeferredScopedRelationshipFactRecordsQuery
// as it exists in production TODAY — against IDENTICAL params and a fixture
// corpus covering every carve-out named in the fix's acceptance criteria, and
// asserts the returned fact_id SETS are IDENTICAL (exact equality, not merely
// superset/subset). It then asserts relationships.DiscoverEvidence produces the
// same evidence over both loaded sets, proving end-to-end equivalence, not only
// row-set equivalence.
//
// This test is written to FAIL if the hoist rewrite changes which facts are
// selected: mutate either predicate arm (drop the own_repo_id COALESCE, break
// the self-exclusion comparison, or change the LIKE escaping) and the fact_id
// sets or the discovered evidence will diverge.
func TestDeferredScopedFactLoadHoistExactlyEquivalentToPreHoistShape(t *testing.T) {
	dsn := dsnForDeferredHoistProof(t)
	ctx := context.Background()
	db := openDeferredHoistProofDB(t, dsn)
	provisionDeferredHoistSchema(t, db)

	seedDeferredHoistFixture(t, ctx, db)
	catalog := deferredHoistCatalog()

	params, ok := buildDeferredScopedFactQueryParams(catalog)
	if !ok {
		t.Fatal("expected usable catalog-derived query params")
	}

	const gitScope = "git-repository-scope:github.com/org/app"
	const gcpScope = "gcp:project:hoist:relationship:global"

	oldGitLoad := runPreHoistDeferredScopedQuery(t, ctx, db, params, gitScope, "gen-hoist")
	newGitLoad := runHoistedDeferredScopedQuery(t, ctx, db, params, gitScope, "gen-hoist")

	oldGCPLoad := runPreHoistDeferredScopedQuery(t, ctx, db, params, gcpScope, "gen-gcp-hoist")
	newGCPLoad := runHoistedDeferredScopedQuery(t, ctx, db, params, gcpScope, "gen-gcp-hoist")

	oldIDs := append(factIDSet(oldGitLoad), factIDSet(oldGCPLoad)...)
	newIDs := append(factIDSet(newGitLoad), factIDSet(newGCPLoad)...)
	sort.Strings(oldIDs)
	sort.Strings(newIDs)

	if len(oldIDs) == 0 {
		t.Fatal("expected the pre-hoist shape to select at least one fact from the fixture")
	}
	if !equalStringSlices(oldIDs, newIDs) {
		t.Fatalf("fact_id sets diverge between pre-hoist and hoisted query shapes\nold: %v\nnew: %v", oldIDs, newIDs)
	}

	// Every carve-out named in the fix's acceptance criteria must survive.
	wantPresent := []string{
		"fact-app-refs-app-config",            // prefix-overlap cross-repo repo_id reference
		"fact-argocd-application",             // ArgoCD unconditional over-select marker
		"fact-gcp-empty-repo-id",              // cloud-scope fact with empty repo_id
		"fact-orphan-repo-references-catalog", // repo_id mismatched to any catalog entry, still references a real target
	}
	newIDSet := make(map[string]bool, len(newIDs))
	for _, id := range newIDs {
		newIDSet[id] = true
	}
	for _, want := range wantPresent {
		if !newIDSet[want] {
			t.Errorf("expected hoisted query to load %q, it was dropped", want)
		}
	}
	wantAbsent := []string{
		"fact-self-mention-only", // pure self-match must stay excluded
		"fact-no-match-noise",    // references nothing in the catalog
	}
	for _, absent := range wantAbsent {
		if newIDSet[absent] {
			t.Errorf("expected hoisted query to EXCLUDE %q, it was loaded", absent)
		}
	}

	// End-to-end equivalence: DiscoverEvidence over the old-shape load and the
	// new-shape load must agree exactly.
	oldEvidence := relationships.DedupeEvidenceFacts(relationships.DiscoverEvidence(append(oldGitLoad, oldGCPLoad...), catalog))
	newEvidence := relationships.DedupeEvidenceFacts(relationships.DiscoverEvidence(append(newGitLoad, newGCPLoad...), catalog))
	if !evidenceSetsEqual(oldEvidence, newEvidence) {
		t.Fatalf("discovered evidence diverges between pre-hoist and hoisted query shapes\nold: %v\nnew: %v", oldEvidence, newEvidence)
	}
	if len(newEvidence) == 0 {
		t.Fatal("expected non-empty discovered evidence from the representative fixture")
	}
}

func evidenceSetsEqual(a, b []relationships.EvidenceFact) bool {
	if len(a) != len(b) {
		return false
	}
	key := func(f relationships.EvidenceFact) string {
		return string(f.EvidenceKind) + "|" + f.SourceRepoID + "|" + f.TargetRepoID + "|" + fmt.Sprint(f.Details["path"]) + "|" + fmt.Sprint(f.Details["matched_value"])
	}
	seenA := make(map[string]int, len(a))
	for _, f := range a {
		seenA[key(f)]++
	}
	for _, f := range b {
		k := key(f)
		if seenA[k] == 0 {
			return false
		}
		seenA[k]--
	}
	for _, count := range seenA {
		if count != 0 {
			return false
		}
	}
	return true
}
