// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
)

// supplyChainImpactFixtureScope seeds one ingestion scope whose active
// generation is activeGeneration, plus any extra generations (superseded or
// non-active) listed with their status.
func supplyChainImpactFixtureScope(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	scopeID, sourceKey, scopePayload, activeGeneration string,
	extra map[string]string,
) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
  scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, active_generation_id, payload
) VALUES ($1, 'synthetic', 'synthetic', $2, 'synthetic', $1,
  '2026-10-23T12:00:00Z', '2026-10-23T12:00:00Z', 'active', NULLIF($3, ''), $4::jsonb)`,
		scopeID, sourceKey, activeGeneration, scopePayload); err != nil {
		t.Fatalf("seed scope %s: %v", scopeID, err)
	}
	generations := map[string]string{}
	for generation, status := range extra {
		generations[generation] = status
	}
	if activeGeneration != "" {
		generations[activeGeneration] = "active"
	}
	for generation, status := range generations {
		if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (
  generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload
) VALUES ($1, $2, 'synthetic', '2026-10-23T12:00:00Z', '2026-10-23T12:00:00Z', $3, '{}'::jsonb)`,
			generation, scopeID, status); err != nil {
			t.Fatalf("seed generation %s: %v", generation, err)
		}
	}
}

// supplyChainImpactFixtureFact seeds one fact row.
func supplyChainImpactFixtureFact(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	factID, scopeID, generationID, factKind, payload string,
	tombstone bool,
) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  schema_version, collector_kind, fencing_token, source_confidence,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload
) VALUES ($1, $2, $3, $4, 'stable:' || $1, '1.0.0', 'synthetic', 1, 'reported',
  'synthetic', 'source:' || $1, '2026-10-23T12:00:00Z', '2026-10-23T12:00:00Z', $5, $6::jsonb)`,
		factID, scopeID, generationID, factKind, tombstone, payload); err != nil {
		t.Fatalf("seed fact %s: %v", factID, err)
	}
}

// supplyChainImpactFixtureIdentity seeds count canonical identity support rows
// into the stub the identity function reads. Fact IDs use fixed-width hex
// components so convert_to order equals the loader's cursor order.
func supplyChainImpactFixtureIdentity(t *testing.T, ctx context.Context, database *sql.DB, scopeID, generationID string, count int) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
INSERT INTO supply_chain_impact_identity_stub (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  schema_version, collector_kind, fencing_token, source_confidence,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload
)
SELECT
  'reducer_container_image_identity_support:' || encode(convert_to($1, 'UTF8'), 'hex') || ':' ||
    encode(convert_to(lpad(n::text, 6, '0'), 'UTF8'), 'hex') || ':' || md5(n::text) || md5(n::text || 'x'),
  $1, $2, 'reducer_container_image_identity', 'stable:identity:' || n, '1.0.0', 'synthetic', 1,
  'reported', 'synthetic', 'source:identity:' || n, '2026-10-23T12:00:00Z',
  '2026-10-23T12:00:00Z', FALSE, jsonb_build_object('ordinal', n)
FROM generate_series(1, $3::integer) AS n`, scopeID, generationID, count); err != nil {
		t.Fatalf("seed identity stub rows: %v", err)
	}
}

// supplyChainImpactFixtureAnalyze refreshes planner statistics after a seed.
func supplyChainImpactFixtureAnalyze(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	for _, table := range []string{"fact_records", "ingestion_scopes", "scope_generations"} {
		if _, err := database.ExecContext(ctx, "VACUUM (ANALYZE) "+table); err != nil {
			t.Fatalf("vacuum analyze %s: %v", table, err)
		}
	}
}

// seedSupplyChainImpactScaledCorpus seeds the #8880 shape: repoScopes git
// scopes, each with filesPerScope TOASTed file facts in its active generation
// and as many in a superseded generation, plus a modest vulnerability,
// reducer, and KEV population. It returns the repository IDs of the first
// requested repositories and the npm filter values that match them.
func seedSupplyChainImpactScaledCorpus(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	repoScopes, filesPerScope, kevRows int,
) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
  scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, active_generation_id, payload
)
SELECT format('git:scaled:%s', lpad(n::text, 4, '0')), 'repository', 'git',
  format('repository:scaled:%s', lpad(n::text, 4, '0')), 'git', format('p%s', n % 8),
  '2026-10-23T12:00:00Z', '2026-10-23T12:00:00Z', 'active',
  format('gen:scaled:%s:active', lpad(n::text, 4, '0')),
  jsonb_build_object('repo_id', format('repository:scaled:%s', lpad(n::text, 4, '0')))
FROM generate_series(1, $1::integer) AS n`, repoScopes); err != nil {
		t.Fatalf("seed scaled scopes: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
SELECT format('gen:scaled:%s:%s', lpad(n::text, 4, '0'), state), format('git:scaled:%s', lpad(n::text, 4, '0')),
  'synthetic', '2026-10-23T12:00:00Z', '2026-10-23T12:00:00Z',
  CASE state WHEN 'active' THEN 'active' ELSE 'superseded' END, '{}'::jsonb
FROM generate_series(1, $1::integer) AS n CROSS JOIN (VALUES ('active'), ('old')) AS states(state)`, repoScopes); err != nil {
		t.Fatalf("seed scaled generations: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  schema_version, collector_kind, fencing_token, source_confidence,
  source_system, source_fact_key, observed_at, ingested_at, payload
)
SELECT
  format('fact:file:%s:%s:%s', lpad(s::text, 4, '0'), state, lpad(f::text, 5, '0')),
  format('git:scaled:%s', lpad(s::text, 4, '0')),
  format('gen:scaled:%s:%s', lpad(s::text, 4, '0'), state),
  'file', format('stable:file:%s:%s', s, f), '1.0.0', 'git', 1, 'reported', 'git',
  format('source:file:%s:%s:%s', s, state, f), '2026-10-23T12:00:00Z', '2026-10-23T12:00:00Z',
  jsonb_build_object(
    'repo_id', format('repository:scaled:%s', lpad(s::text, 4, '0')),
    'relative_path', format('src/f_%s.%s', f, CASE WHEN f % 4 = 0 THEN 'ts' ELSE 'go' END),
    'is_dependency', false,
    'parsed_file_data', jsonb_build_object(
      'language', CASE WHEN f % 4 = 0 THEN 'typescript' ELSE 'go' END,
      -- distinct md5 blocks keep the payload out of line (TOAST), which is
      -- the per-predicate detoast mechanism #5237 and #8880 measure.
      'content', (SELECT string_agg(md5(s::text || ':' || f::text || ':' || g::text), '') FROM generate_series(1, 160) AS g)
    )
  )
FROM generate_series(1, $1::integer) AS s
CROSS JOIN generate_series(1, $2::integer) AS f
CROSS JOIN (VALUES ('active'), ('old')) AS states(state)`, repoScopes, filesPerScope); err != nil {
		t.Fatalf("seed scaled file facts: %v", err)
	}
	supplyChainImpactFixtureScope(t, ctx, database, "osv:scaled", "osv:scaled", `{}`, "gen:osv:scaled", nil)
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  schema_version, collector_kind, fencing_token, source_confidence,
  source_system, source_fact_key, observed_at, ingested_at, payload
)
SELECT format('fact:%s:%s', kind, lpad(n::text, 5, '0')), 'osv:scaled', 'gen:osv:scaled', kind,
  format('stable:%s:%s', kind, n), '1.0.0', 'osv', 1, 'reported', 'osv',
  format('source:%s:%s', kind, n), '2026-10-23T12:00:00Z', '2026-10-23T12:00:00Z',
  CASE kind
    WHEN 'vulnerability.affected_package' THEN jsonb_build_object(
      'package_id', format('npm://registry.npmjs.org/pkg-%s', n % 40),
      'purl', format('pkg:npm/pkg-%s', n % 40),
      'cve_id', format('CVE-2026-%s', lpad(n::text, 5, '0')),
      'advisory_id', format('GHSA-scaled-%s', n))
    WHEN 'vulnerability.cve' THEN jsonb_build_object(
      'cve_id', format('CVE-2026-%s', lpad(n::text, 5, '0')),
      'advisory_id', format('GHSA-scaled-%s', n))
    ELSE jsonb_build_object('cve_id', format('CVE-2026-%s', lpad(n::text, 5, '0')))
  END
FROM generate_series(1, $1::integer) AS n
CROSS JOIN (VALUES ('vulnerability.affected_package'), ('vulnerability.cve'), ('vulnerability.known_exploited')) AS kinds(kind)`, kevRows); err != nil {
		t.Fatalf("seed scaled vulnerability facts: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  schema_version, collector_kind, fencing_token, source_confidence,
  source_system, source_fact_key, observed_at, ingested_at, payload
)
SELECT format('fact:%s:%s', kind, lpad(s::text, 4, '0')), format('git:scaled:%s', lpad(s::text, 4, '0')),
  format('gen:scaled:%s:active', lpad(s::text, 4, '0')), kind, format('stable:%s:%s', kind, s),
  '1.0.0', 'reducer', 1, 'derived', 'reducer', format('source:%s:%s', kind, s),
  '2026-10-23T12:00:00Z', '2026-10-23T12:00:00Z',
  jsonb_build_object('scope_id', format('git:scaled:%s', lpad(s::text, 4, '0')), 'canonical_id', format('c:%s', s))
FROM generate_series(1, $1::integer) AS s
CROSS JOIN (VALUES ('reducer_workload_identity'), ('reducer_platform_materialization')) AS kinds(kind)`, repoScopes); err != nil {
		t.Fatalf("seed scaled reducer facts: %v", err)
	}
	supplyChainImpactFixtureAnalyze(t, ctx, database)
}

// supplyChainImpactScaledRepositoryID names scaled repository n.
func supplyChainImpactScaledRepositoryID(n int) string {
	return fmt.Sprintf("repository:scaled:%04d", n)
}
