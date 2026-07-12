// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

const familyIDProofCandidateTableSQL = `
DROP TABLE IF EXISTS tmp_relationship_family_fact_ids;
CREATE TEMP TABLE tmp_relationship_family_fact_ids AS
SELECT fact.fact_id, fact.scope_id, fact.generation_id
FROM fact_records AS fact
WHERE fact.fact_kind IN ('content', 'file', 'gcp_cloud_relationship')
  AND (
    fact.fact_kind = 'gcp_cloud_relationship'
    OR lower(COALESCE(fact.payload->>'artifact_type', '')) IN (
      'terraform',
      'terraform_hcl',
      'terraform_template_text',
      'terragrunt',
      'helm',
      'argocd',
      'dockerfile',
      'docker_compose',
      'github_actions_workflow'
    )
    OR lower(COALESCE(fact.payload->>'artifact_type', '')) LIKE 'ansible_%'
    OR lower(COALESCE(
      fact.payload->>'relative_path',
      fact.payload->>'content_path',
      fact.payload->>'file_path',
      fact.payload->>'path',
      ''
    )) ~ '(^|/)(dockerfile|jenkinsfile|puppetfile|berksfile)$|(^|/)docker-compose\.ya?ml$|(^|/)compose\.ya?ml$|(^|/)\.github/workflows/[^/]+\.ya?ml$|(^|/)applicationsets?/.*\.ya?ml$|(^|/)argocd/.*\.ya?ml$|(^|/)values([^/]*)\.ya?ml$|(^|/)chart\.ya?ml$|(^|/)kustomization\.ya?ml$|(^|/)(playbooks|roles|group_vars|host_vars|inventories)/|(^|/)inventory($|/)|\.(tf|tf\.json|tfvars|tfvars\.json|hcl|tpl)$'
    OR CASE
      WHEN lower(COALESCE(fact.payload->>'artifact_type', '')) = 'argocd'
        OR lower(COALESCE(
          fact.payload->>'relative_path',
          fact.payload->>'content_path',
          fact.payload->>'file_path',
          fact.payload->>'path',
          ''
        )) ~ '\.ya?ml$'
      THEN lower(COALESCE(
        fact.payload->>'content',
        fact.payload->>'content_body',
        ''
      )) LIKE '%kind: application%'
        OR lower(COALESCE(
          fact.payload->>'content',
          fact.payload->>'content_body',
          ''
        )) LIKE '%kind: applicationset%'
      ELSE FALSE
    END
  );
CREATE INDEX tmp_relationship_family_fact_ids_partition_idx
    ON tmp_relationship_family_fact_ids(scope_id, generation_id, fact_id);
ANALYZE tmp_relationship_family_fact_ids;
`

const familyIDProofAliasOnlyCandidateQuery = latestGenerationCTE + `,
arg_types AS (
    SELECT $2::text[] AS repo_ids
),
matched_fact_ids AS MATERIALIZED (
    SELECT fact.fact_id
    FROM tmp_relationship_family_fact_ids AS family
    JOIN fact_records AS fact
      ON fact.fact_id = family.fact_id
    JOIN arg_types ON TRUE
    WHERE family.scope_id = $3
      AND family.generation_id = $4
      AND lower(fact.payload::text) LIKE ANY($1)
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
    COALESCE(fact.source_uri, '') AS source_uri,
    COALESCE(fact.source_record_id, '') AS source_record_id,
    fact.observed_at,
    fact.is_tombstone,
    fact.payload
FROM fact_records AS fact
JOIN matched_fact_ids AS matched
  ON matched.fact_id = fact.fact_id
JOIN latest_generations AS latest
  ON latest.scope_id = fact.scope_id
 AND latest.generation_id = fact.generation_id
WHERE latest.generation_id IS NOT NULL
ORDER BY fact.observed_at ASC, fact.fact_id ASC
`

func TestDeferredRelationshipFamilyIDSurfaceProof(t *testing.T) {
	dsn := dsnForFamilyIDProof(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	db := openDeferredHoistProofDB(t, dsn)
	catalog, err := loadRepositoryCatalog(ctx, SQLDB{DB: db})
	if err != nil {
		t.Fatalf("load repository catalog: %v", err)
	}
	params, ok := buildDeferredScopedFactQueryParams(catalog)
	if !ok {
		t.Fatal("expected usable retained-DB catalog params")
	}

	buildStart := time.Now()
	if _, err := db.ExecContext(ctx, familyIDProofCandidateTableSQL); err != nil {
		t.Fatalf("build family id candidate table: %v", err)
	}
	t.Logf("family id surface build=%s", time.Since(buildStart))

	for _, scopeID := range familyIDProofScopes() {
		generationID := activeGenerationForFamilyIDProofScope(t, ctx, db, scopeID)

		oldStart := time.Now()
		oldLoad := runHoistedDeferredScopedQuery(t, ctx, db, params, scopeID, generationID)
		oldDuration := time.Since(oldStart)

		newStart := time.Now()
		newLoad := runFamilyIDCandidateQuery(t, ctx, db, params, scopeID, generationID)
		newDuration := time.Since(newStart)

		oldEvidence := relationships.DedupeEvidenceFacts(relationships.DiscoverEvidence(oldLoad, catalog))
		newEvidence := relationships.DedupeEvidenceFacts(relationships.DiscoverEvidence(newLoad, catalog))
		if !evidenceSetsEqual(oldEvidence, newEvidence) {
			t.Fatalf(
				"family-id candidate evidence diverged for scope=%s generation=%s old_facts=%d new_facts=%d old_evidence=%d new_evidence=%d",
				scopeID,
				generationID,
				len(oldLoad),
				len(newLoad),
				len(oldEvidence),
				len(newEvidence),
			)
		}
		if os.Getenv("ESHU_RELATIONSHIP_FAMILY_ID_PROOF_PRODUCTION_EQUIV") != "1" &&
			scopeID == "git-repository-scope:repository:r_de3355a0" && newDuration >= oldDuration {
			t.Fatalf("task777 family-id candidate did not improve load time: old=%s new=%s", oldDuration, newDuration)
		}
		t.Logf(
			"family-id proof scope=%s generation=%s old_facts=%d new_facts=%d old_evidence=%d new_evidence=%d old_load=%s new_load=%s",
			scopeID,
			generationID,
			len(oldLoad),
			len(newLoad),
			len(oldEvidence),
			len(newEvidence),
			oldDuration,
			newDuration,
		)
	}
}

func dsnForFamilyIDProof(t *testing.T) string {
	t.Helper()
	if dsn := os.Getenv("ESHU_RELATIONSHIP_FAMILY_ID_PROOF_DSN"); dsn != "" {
		return dsn
	}
	if dsn := os.Getenv("ESHU_DEFERRED_PARTITION_PROOF_DSN"); dsn != "" {
		return dsn
	}
	if dsn := os.Getenv("ESHU_LATEST_GENERATION_PROOF_DSN"); dsn != "" {
		return dsn
	}
	t.Skip("set ESHU_RELATIONSHIP_FAMILY_ID_PROOF_DSN to run the retained DB family-id proof")
	return ""
}

func familyIDProofScopes() []string {
	raw := os.Getenv("ESHU_RELATIONSHIP_FAMILY_ID_PROOF_SCOPES")
	if raw == "" {
		return []string{
			"git-repository-scope:repository:r_de3355a0",
			"git-repository-scope:repository:r_d393ab02",
			"git-repository-scope:repository:r_415c2ca3",
			"git-repository-scope:repository:r_d737ae8e",
		}
	}
	parts := strings.Split(raw, ",")
	scopes := make([]string, 0, len(parts))
	for _, part := range parts {
		scope := strings.TrimSpace(part)
		if scope != "" {
			scopes = append(scopes, scope)
		}
	}
	return scopes
}

func activeGenerationForFamilyIDProofScope(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
) string {
	t.Helper()
	var generation sql.NullString
	if err := db.QueryRowContext(
		ctx,
		"SELECT active_generation_id FROM ingestion_scopes WHERE scope_id = $1",
		scopeID,
	).Scan(&generation); err != nil {
		t.Fatalf("load active generation for scope %s: %v", scopeID, err)
	}
	if !generation.Valid || strings.TrimSpace(generation.String) == "" {
		t.Fatalf("scope %s has no active_generation_id", scopeID)
	}
	return generation.String
}

func runFamilyIDCandidateQuery(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	params deferredScopedFactQueryParams,
	scopeID, generationID string,
) []facts.Envelope {
	t.Helper()
	rows, err := db.QueryContext(
		ctx,
		familyIDProofAliasOnlyCandidateQuery,
		params.nonRepoIDLike,
		params.repoIDValues,
		scopeID,
		generationID,
	)
	if err != nil {
		t.Fatalf("query family-id alias-only candidate: %v", err)
	}
	return collectDeferredScopedRows(t, rows)
}
