// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

const deferredHoistMigrationReferenceKeySeedSQL = `
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
        REGEXP_REPLACE(
            REGEXP_REPLACE(LOWER(payload::text), '[^a-z0-9._-]+', '|', 'g'),
            '\.git(\||$)',
            '\1',
            'g'
        ),
        '\.(yaml|yml|json|tfvars|tf|hcl)(\||$)',
        '\2',
        'g'
    ) ||
    '|'
FROM fact_records
WHERE fact_kind IN ('content', 'file', 'gcp_cloud_relationship')
  AND is_tombstone = FALSE
`

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
		{RepoID: "github.com/org/.github", Aliases: []string{"github.com/org/.github"}},
		{RepoID: "repo-noise-target", Aliases: []string{"repo-noise-target"}},
	}
}

func TestDeferredScopedFactLoadMigrationReferenceKeysCoverTokenizationEdges(t *testing.T) {
	dsn := dsnForDeferredHoistProof(t)
	ctx := context.Background()
	db := openDeferredHoistProofDB(t, dsn)
	provisionDeferredHoistSchema(t, db)

	seedDeferredHoistFixture(t, ctx, db)
	if _, err := db.ExecContext(ctx, "TRUNCATE relationship_reference_candidate_keys"); err != nil {
		t.Fatalf("truncate relationship reference keys: %v", err)
	}
	if _, err := db.ExecContext(ctx, deferredHoistMigrationReferenceKeySeedSQL); err != nil {
		t.Fatalf("seed migration relationship reference keys: %v", err)
	}

	params, ok := buildDeferredScopedFactQueryParams(deferredHoistCatalog())
	if !ok {
		t.Fatal("expected usable catalog-derived query params")
	}
	loaded := runHoistedDeferredScopedQuery(
		t, ctx, db, params, "git-repository-scope:github.com/org/app", "gen-hoist",
	)
	loadedIDSet := make(map[string]bool, len(loaded))
	for _, envelope := range loaded {
		loadedIDSet[envelope.FactID] = true
	}
	for _, want := range []string{
		"fact-app-refs-app-config-yaml",
		"fact-app-refs-dot-github",
	} {
		if !loadedIDSet[want] {
			t.Fatalf("migration-seeded reference keys dropped %q; loaded IDs: %v", want, factIDSet(loaded))
		}
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
