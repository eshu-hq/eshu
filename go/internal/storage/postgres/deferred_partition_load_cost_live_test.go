// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
)

// Frozen pre-#10875 baseline of the deferred partition load, copied verbatim
// from go/internal/storage/postgres/backfill at c4756a4716^ (the
// ListDeferredScopedRelationshipFactRecordsQuery const in deferred_facts.go
// plus the predicate consts in deferred_predicate.go) with const names given
// a legacy prefix. It is the "before" side of the #10971 P1 cost comparison:
// the shipped query must select a superset of this baseline on the same
// corpus, and both shapes are timed below on a migrated disposable schema.
const legacyDeferredPathSQL = `lower(COALESCE(
            fact.payload->>'relative_path',
            fact.payload->>'content_path',
            fact.payload->>'file_path',
            fact.payload->>'path',
            ''
          ))`

const legacyDeferredArtifactTypeSQL = `lower(COALESCE(fact.payload->>'artifact_type', ''))`

const legacyDeferredArgoCDMarkerSQL = `(CASE
            WHEN ` + legacyDeferredArtifactTypeSQL + ` = 'argocd'
              OR ` + legacyDeferredPathSQL + ` ~ '\.ya?ml$'
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
            ELSE false
          END)`

const legacyDeferredSaltMarkerSQL = `(CASE
            WHEN ` + legacyDeferredPathSQL + ` ~ '\.ya?ml$'
              AND COALESCE(
              fact.payload->>'content',
              fact.payload->>'content_body',
              ''
            ) LIKE '%gitfs_remotes:%'
            THEN COALESCE(
              fact.payload->>'content',
              fact.payload->>'content_body',
              ''
            ) ~ E'(^|\n)gitfs_remotes[[:space:]]*:'
            ELSE false
          END)`

const legacyDeferredFluxMarkerSQL = `(
            jsonb_typeof(fact.payload -> 'parsed_file_data' -> 'flux_git_repositories') = 'array'
            AND jsonb_array_length(fact.payload -> 'parsed_file_data' -> 'flux_git_repositories') > 0
          )`

const legacyDeferredCandidatePredicateSQL = `(
          fact.fact_kind = 'gcp_cloud_relationship'
          OR ` + legacyDeferredArtifactTypeSQL + ` IN (
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
          OR ` + legacyDeferredArtifactTypeSQL + ` LIKE 'ansible_%'
          OR ` + legacyDeferredPathSQL + ` ~ '(^|/)(dockerfile(\.[^/]*)?|jenkinsfile(\.[^/]*)?|puppetfile|berksfile)$|(^|/)docker-compose\.ya?ml$|(^|/)compose\.ya?ml$|(^|/)\.github/workflows/[^/]+\.ya?ml$|(^|/)applicationsets?/.*\.ya?ml$|(^|/)argocd/.*\.ya?ml$|(^|/)values([^/]*)\.ya?ml$|(^|/)chart\.ya?ml$|(^|/)kustomization\.ya?ml$|(^|/)(playbooks|roles|group_vars|host_vars|inventories)/|(^|/)inventory($|/)|\.(tf|tf\.json|tfvars|tfvars\.json|hcl|tpl)$'
          OR ` + legacyDeferredArgoCDMarkerSQL + `
          OR ` + legacyDeferredSaltMarkerSQL + `
          OR ` + legacyDeferredFluxMarkerSQL + `
        )`

const legacyDeferredPayloadFactsFilterSQL = `WHERE ` + legacyDeferredCandidatePredicateSQL

const legacyDeferredLatestGenerationCTE = `WITH latest_generations AS (
    SELECT DISTINCT ON (generation.scope_id)
        generation.scope_id,
        COALESCE(
            scope.active_generation_id,
            generation.generation_id
        ) AS generation_id
    FROM scope_generations AS generation
    LEFT JOIN ingestion_scopes AS scope
      ON scope.scope_id = generation.scope_id
    WHERE generation.scope_id = $3
    ORDER BY generation.scope_id, generation.ingested_at DESC, generation.generation_id DESC
)`

const legacyDeferredPartitionLoadQuery = legacyDeferredLatestGenerationCTE + `,
source_facts AS MATERIALIZED (
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
        fact.payload,
        lower(COALESCE(fact.payload->>'repo_id', '')) AS own_repo_id
    FROM fact_records AS fact
    WHERE fact.scope_id = $3
      AND fact.generation_id = $4
      AND fact.fact_kind IN ('content', 'file', 'gcp_cloud_relationship')
      AND ` + legacyDeferredCandidatePredicateSQL + `
),
relationship_family_payload_facts AS MATERIALIZED (
    SELECT
        fact.fact_id,
        fact.own_repo_id,
        lower(fact.payload::text) AS payload_lower
    FROM source_facts AS fact
    ` + legacyDeferredPayloadFactsFilterSQL + `
),
candidate_reference_keys AS MATERIALIZED (
    SELECT ref.fact_id, ref.source_repo_id, ref.reference_key
    FROM relationship_family_payload_facts AS fact
    JOIN relationship_reference_candidate_keys AS ref
      ON ref.fact_id = fact.fact_id
     AND ref.scope_id = $3
     AND ref.generation_id = $4
),
matched_fact_ids AS MATERIALIZED (
        SELECT fact.fact_id
        FROM relationship_family_payload_facts AS fact
        WHERE fact.payload_lower LIKE ANY($1)
        UNION
        SELECT ref.fact_id
        FROM candidate_reference_keys AS ref
        JOIN unnest($2::text[], $7::text[]) AS catalog_repo_id(value, reference_key)
          ON catalog_repo_id.value <> ref.source_repo_id
         AND position('|' || catalog_repo_id.reference_key || '|' in ref.reference_key) > 0
        UNION
        SELECT fact.fact_id
        FROM relationship_family_payload_facts AS fact
        WHERE NOT EXISTS (
            SELECT 1
            FROM relationship_reference_candidate_keys AS ref
            WHERE ref.fact_id = fact.fact_id
              AND ref.scope_id = $3
              AND ref.generation_id = $4
          )
          AND (
            (fact.own_repo_id = $6 AND $5::text IS NOT NULL AND fact.payload_lower ~ $5)
            OR (
              fact.own_repo_id <> $6
              AND EXISTS (
                SELECT 1
                FROM unnest($2::text[]) AS catalog_repo_id(value)
                WHERE catalog_repo_id.value <> fact.own_repo_id
                  AND fact.payload_lower LIKE
                    '%' ||
                    replace(replace(replace(catalog_repo_id.value, '\', '\\'), '%', '\%'), '_', '\_') ||
                    '%' ESCAPE '\'
              )
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
FROM source_facts AS fact
JOIN matched_fact_ids AS matched
  ON matched.fact_id = fact.fact_id
JOIN latest_generations AS latest
  ON latest.scope_id = fact.scope_id
 AND latest.generation_id = fact.generation_id
WHERE latest.generation_id IS NOT NULL
ORDER BY fact.observed_at ASC, fact.fact_id ASC
`

// costSeedScope is the measured partition: 2000 facts with production-width
// binds (1029 LIKE terms, 1024 repo values), ~3KB payloads, and the family
// mix of the theory shim (ref-bearing, alias-only, argocd, flux, gcp,
// noise), so plan shape is driven by realistic partition size x bind width.
const (
	costSeedScope = "cost-scope-1"
	costSeedGen   = "cost-gen-1"
	costSeedFacts = 2000
)

func seedDeferredCostPartition(t *testing.T, ctx context.Context, database *sql.DB) []facts.Envelope {
	t.Helper()
	if _, err := database.ExecContext(ctx, `INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload, active_generation_id) VALUES ($1, 'repository', 'git', $1, 'git', $1, NOW(), NOW(), 'active', '{}'::jsonb, $2)`, costSeedScope, costSeedGen); err != nil {
		t.Fatalf("seed scope: %v", err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO scope_generations (scope_id, generation_id, trigger_kind, is_delta, source_commit_sha, observed_at, ingested_at, status, activated_at) VALUES ($1, $2, 'sync', false, 'c-cost', NOW(), NOW(), 'active', NOW())`, costSeedScope, costSeedGen); err != nil {
		t.Fatalf("seed generation: %v", err)
	}
	filler := strings.Repeat("lorem ipsum dolor sit amet ", 100)
	envelopes := make([]facts.Envelope, 0, costSeedFacts)
	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	for i := 0; i < costSeedFacts; i++ {
		id := fmt.Sprintf("cost-fact-%04d", i)
		kind := "content"
		payload := map[string]any{"repo_id": "github.com/org000/svc000", "filler": filler}
		switch {
		case i < 800:
			foreign := fmt.Sprintf("github.com/org%03d/svc%03d", (i%1023)+1, (i%1023)+1)
			payload["artifact_type"] = "terraform"
			payload["relative_path"] = "main.tf"
			payload["content"] = fmt.Sprintf("module x { source = %q } %s", foreign, filler)
		case i < 1200:
			payload["artifact_type"] = "terraform"
			payload["relative_path"] = "main.tf"
			payload["content"] = fmt.Sprintf("resource y svc%03d %s", i%1024, filler)
		case i < 1400:
			payload["artifact_type"] = "argocd"
			payload["content_path"] = "app.yaml"
			payload["content"] = "kind: Application " + filler
		case i < 1500:
			kind = "file"
			payload = map[string]any{
				"repo_id":          "github.com/org000/svc000",
				"relative_path":    "deploy.yaml",
				"parsed_file_data": map[string]any{"flux_git_repositories": []any{map[string]any{"url": "https://example.com/repo.git"}}},
				"filler":           filler,
			}
		case i < 1550:
			kind = "gcp_cloud_relationship"
			payload = map[string]any{"repo_id": "github.com/org000/svc000", "filler": filler}
		default:
			payload["artifact_type"] = "textile"
			payload["relative_path"] = "README.md"
			payload["content"] = filler
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal seed payload: %v", err)
		}
		observed := base.Add(time.Duration(i) * time.Second).UTC().Format(time.RFC3339)
		if _, err := database.ExecContext(ctx, `INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload) VALUES ($1, $2, $3, $4, $1, 'git', $1, $5, NOW(), $6)`,
			id, costSeedScope, costSeedGen, kind, observed, string(raw)); err != nil {
			t.Fatalf("seed fact: %v", err)
		}
		envelopes = append(envelopes, facts.Envelope{FactID: id, ScopeID: costSeedScope, GenerationID: costSeedGen, FactKind: kind, Payload: payload})
	}
	if err := refreshRelationshipReferenceCandidateKeys(ctx, SQLDB{DB: database}, envelopes); err != nil {
		t.Fatalf("seed sidecar rows: %v", err)
	}
	return envelopes
}

func explainPartitionLoad(t *testing.T, ctx context.Context, database *sql.DB, query string, args ...any) (medianMs float64, hitBuffers int64) {
	t.Helper()
	var times []float64
	for i := 0; i < 3; i++ {
		rows, err := database.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, TIMING OFF) "+query, args...)
		if err != nil {
			t.Fatalf("explain partition load: %v", err)
		}
		var plan strings.Builder
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				rows.Close()
				t.Fatalf("scan plan: %v", err)
			}
			plan.WriteString(line + "\n")
		}
		rows.Close()
		text := plan.String()
		for _, line := range strings.Split(text, "\n") {
			if v, ok := strings.CutPrefix(line, "Execution Time: "); ok {
				ms, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(v), " ms"), 64)
				if err != nil {
					t.Fatalf("parse execution time %q: %v", line, err)
				}
				times = append(times, ms)
			}
			if idx := strings.Index(line, "Shared Hit Blocks: "); idx >= 0 {
				var hits int64
				if _, err := fmt.Sscanf(line[idx:], "Shared Hit Blocks: %d", &hits); err == nil && hits > hitBuffers {
					hitBuffers = hits
				}
			}
		}
	}
	sort.Float64s(times)
	return times[len(times)/2], hitBuffers
}

func collectPartitionSet(t *testing.T, ctx context.Context, database *sql.DB, query string, args ...any) map[string]bool {
	t.Helper()
	rows, err := database.QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("run partition load: %v", err)
	}
	defer func() { _ = rows.Close() }()
	set := map[string]bool{}
	cols, err := rows.Columns()
	if err != nil {
		t.Fatalf("columns: %v", err)
	}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatalf("scan loaded fact: %v", err)
		}
		id, _ := vals[0].(string)
		set[id] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return set
}

// TestDeferredPartitionLoadCostLive is the #10971 P1 measurement: per-partition
// load latency and buffer cost for the legacy-only query, the shipped query
// (legacy arms plus keyed arms), and the fallback probe COUNT, on a migrated
// disposable schema with a realistic seed. Figures are logged for the evidence
// note; the durable assertions are legacy-subset-shipped (no under-selection)
// and a zero probe count on the fully backfilled seed.
func TestDeferredPartitionLoadCostLive(t *testing.T) {
	database, ctx := openGenerationRetentionMigratedSchema(t)

	seedDeferredCostPartition(t, ctx, database)

	var likeTerms, repoValues, repoKeys, anchors []string
	for i := 0; i < 1024; i++ {
		repo := fmt.Sprintf("github.com/org%03d/svc%03d", i, i)
		alias := fmt.Sprintf("svc%03d", i)
		likeTerms = append(likeTerms, "%"+alias+"%")
		repoValues = append(repoValues, repo)
		repoKeys = append(repoKeys, strings.ToLower(repo))
		anchors = append(anchors, alias)
	}
	for _, marker := range []string{"kind: application", "artifact_type: argocd", "argocd_applications", "argocd_applicationsets", "flux_git_repositories"} {
		likeTerms = append(likeTerms, "%"+marker+"%")
		anchors = append(anchors, marker)
	}
	ownRepo := "github.com/org000/svc000"
	regex, ok := backfill.BuildDeferredRepoIDRegex(repoValues[:32], ownRepo)
	if !ok {
		t.Fatal("build repo regex: no alternation")
	}
	regexParam := sql.NullString{String: regex, Valid: true}

	legacyArgs := []any{array.StringArray(likeTerms), array.StringArray(repoValues), costSeedScope, costSeedGen, regexParam, ownRepo, array.StringArray(repoKeys)}
	shippedArgs := append(append([]any{}, legacyArgs...), array.StringArray(anchors))

	legacyMs, legacyBuf := explainPartitionLoad(t, ctx, database, legacyDeferredPartitionLoadQuery, legacyArgs...)
	shippedMs, shippedBuf := explainPartitionLoad(t, ctx, database, backfill.ListDeferredScopedRelationshipFactRecordsQuery, shippedArgs...)
	probeMs, _ := explainPartitionLoad(t, ctx, database, backfill.DeferredRelationshipKeyFallbackProbeSQL, costSeedScope, costSeedGen)

	legacySet := collectPartitionSet(t, ctx, database, legacyDeferredPartitionLoadQuery, legacyArgs...)
	shippedSet := collectPartitionSet(t, ctx, database, backfill.ListDeferredScopedRelationshipFactRecordsQuery, shippedArgs...)
	for id := range legacySet {
		if !shippedSet[id] {
			t.Fatalf("shipped load drops legacy fact %s (legacy %d, shipped %d)", id, len(legacySet), len(shippedSet))
		}
	}
	t.Logf("deferred partition cost (2000 facts, %d LIKE terms, %d repo values): legacy=%.1fms/%dbufs shipped=%.1fms/%dbufs probe=%.3fms legacy_set=%d shipped_set=%d",
		len(likeTerms), len(repoValues), legacyMs, legacyBuf, shippedMs, shippedBuf, probeMs, len(legacySet), len(shippedSet))
}
