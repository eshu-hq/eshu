// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// Issue #11102 reported owner scopes pinned at exactly 256 findings per CVE
// and suspected a cut that drops further consuming repositories. These proofs
// run one owner pass of the real handler over a real FactStore and the
// production writer for one advisory, one package and N consuming
// repositories, each in its own repository scope, then rebuild the canonical
// winners table, and require all N findings in the owner scope and N winners
// keys. Two consumer arms run: reducer_package_consumption_correlation facts,
// and git content_entity lockfile dependency rows (the manifest arm).
//
// They pass on main: for this synthetic shape (one advisory, one package, one
// owner pass) neither arm cuts findings or winners keys at 256. Measured on
// the QA read replica on 2026-11-21 with read-only queries: npm
// brace-expansion, the package behind both CVEs the issue names, has 256
// consuming repositories, 252 with active-generation manifest rows plus 4
// reached only through rows in a superseded generation. A query counting
// distinct repository ids per (scope, CVE) group over active-generation
// finding facts, run in 100-scope chunks over all 6172 scopes holding active
// findings, found 428 of 99,713 groups above 256 distinct repositories, max
// 269, so a 256 cap is falsified on QA. The test pins the boundary so a
// future cut fails here.
const (
	ownerCeilingCVE     = "CVE-2026-11102"
	ownerCeilingPackage = "pkg:npm/owner-ceiling"
)

// ownerCeilingSeeder seeds count consuming repositories for one proof arm.
type ownerCeilingSeeder func(t *testing.T, ctx context.Context, db *sql.DB, count int)

// seedOwnerCeilingRepositoryScopes registers count active repository scopes
// (repository:11102:0000 ...), each with one active generation.
func seedOwnerCeilingRepositoryScopes(t *testing.T, ctx context.Context, db *sql.DB, count int) {
	t.Helper()
	statements := []string{`
INSERT INTO ingestion_scopes (
  scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload
)
SELECT 'repository:11102:' || lpad(n::text, 4, '0'), 'repository', 'synthetic',
       'repository:11102:' || lpad(n::text, 4, '0'), 'synthetic',
       'repository:11102:' || lpad(n::text, 4, '0'), $1::timestamptz, $1::timestamptz, 'active', '{}'::jsonb
FROM generate_series(0, $2::integer - 1) AS n`, `
INSERT INTO scope_generations (
  generation_id, scope_id, trigger_kind, observed_at, ingested_at,
  status, activated_at, payload
)
SELECT 'generation:11102:' || lpad(n::text, 4, '0'), 'repository:11102:' || lpad(n::text, 4, '0'),
       'synthetic', $1::timestamptz, $1::timestamptz, 'active', $1::timestamptz, '{}'::jsonb
FROM generate_series(0, $2::integer - 1) AS n`, `
UPDATE ingestion_scopes
SET active_generation_id = 'generation:11102:' || substr(scope_id, length('repository:11102:') + 1)
WHERE scope_id LIKE 'repository:11102:%' AND $1::timestamptz IS NOT NULL AND $2::integer > 0`}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement, replaceSetLiveNow, count); err != nil {
			t.Fatalf("seed %d repository scopes: %v", count, err)
		}
	}
}

// seedOwnerCeilingConsumers is the correlation arm: count repository scopes,
// each holding one reducer_package_consumption_correlation fact that consumes
// ownerCeilingPackage at an affected version.
func seedOwnerCeilingConsumers(t *testing.T, ctx context.Context, db *sql.DB, count int) {
	t.Helper()
	seedOwnerCeilingRepositoryScopes(t, ctx, db, count)
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone,
  payload, fencing_token
)
SELECT 'package-consumption:11102:' || lpad(n::text, 4, '0'),
       'repository:11102:' || lpad(n::text, 4, '0'), 'generation:11102:' || lpad(n::text, 4, '0'),
       'reducer_package_consumption_correlation',
       'package-consumption:11102:' || lpad(n::text, 4, '0'), 'synthetic',
       'package-consumption:11102:' || lpad(n::text, 4, '0'), $1::timestamptz, $1::timestamptz, FALSE,
       jsonb_build_object(
         'package_id', '`+ownerCeilingPackage+`', 'relationship_kind', 'consumption',
         'repository_id', 'repo://owner-ceiling/r' || lpad(n::text, 4, '0'),
         'dependency_range', '1.2.3', 'canonical_writes', 1,
         'evidence_fact_ids', jsonb_build_array('manifest-lock-' || n::text)),
       0
FROM generate_series(0, $2::integer - 1) AS n`, replaceSetLiveNow, count); err != nil {
		t.Fatalf("seed %d consumption correlations: %v", count, err)
	}
}

// seedOwnerCeilingManifestConsumers is the manifest arm, the path the QA
// findings use: count repository scopes, each holding one git content_entity
// npm lockfile dependency row for owner-ceiling at an affected version, in the
// shape the reducer's manifest dependency read selects.
func seedOwnerCeilingManifestConsumers(t *testing.T, ctx context.Context, db *sql.DB, count int) {
	t.Helper()
	seedOwnerCeilingRepositoryScopes(t, ctx, db, count)
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone,
  payload, fencing_token
)
SELECT 'manifest-dependency:11102:' || lpad(n::text, 4, '0'),
       'repository:11102:' || lpad(n::text, 4, '0'), 'generation:11102:' || lpad(n::text, 4, '0'),
       'content_entity',
       'manifest-dependency:11102:' || lpad(n::text, 4, '0'), 'git',
       'manifest-dependency:11102:' || lpad(n::text, 4, '0'), $1::timestamptz, $1::timestamptz, FALSE,
       jsonb_build_object(
         'repo_id', 'repo://owner-ceiling/r' || lpad(n::text, 4, '0'),
         'relative_path', 'package-lock.json',
         'entity_type', 'Variable', 'entity_name', 'owner-ceiling',
         'entity_metadata', jsonb_build_object(
           'config_kind', 'dependency', 'package_manager', 'npm',
           'value', '1.2.3', 'resolved_version', '1.2.3', 'lockfile', true,
           'dependency_scope', 'runtime')),
       0
FROM generate_series(0, $2::integer - 1) AS n`, replaceSetLiveNow, count); err != nil {
		t.Fatalf("seed %d manifest dependencies: %v", count, err)
	}
}

// runOwnerCeilingPass seeds one advisory and count consumers through seed, runs one owner
// pass and the winners rebuild, and returns the owner scope's active finding
// count, the winners key count for the CVE, and the pass's sub-signals.
func runOwnerCeilingPass(t *testing.T, seed ownerCeilingSeeder, count int) (int, int, map[string]float64) {
	t.Helper()
	_ = supplyChainImpactLiveDSN(t, "#11102 owner-ceiling proof")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	// The fully migrated isolated schema: the winners rebuild reads scanner
	// tables the bootstrap-only schema does not carry.
	db := openAdvisorySuppressionIsolatedDB(t)
	t.Cleanup(func() { _ = db.Close() })
	replaceSetSeedScope(t, ctx, db)
	cappedSeedIntelFact(t, ctx, db, "vuln-cve:11102", "vulnerability.cve", fmt.Sprintf(
		`{"cve_id":%q,"advisory_id":%q,"cvss_score":8.1,"aliases":[%q]}`,
		ownerCeilingCVE, ownerCeilingCVE, ownerCeilingCVE))
	cappedSeedIntelFact(t, ctx, db, "vuln-affected:11102", "vulnerability.affected_package", fmt.Sprintf(
		`{"cve_id":%q,"advisory_id":%q,"package_id":%q,
		  "ecosystem":"npm","package_name":"owner-ceiling","affected_versions":["1.2.3"],"fixed_versions":["1.3.0"]}`,
		ownerCeilingCVE, ownerCeilingCVE, ownerCeilingPackage))
	seed(t, ctx, db, count)

	handler := supplychaincore.SupplyChainImpactHandler{
		FencingTokenIssuer: postgres.PostgresSupplyChainImpactFencingTokenIssuer{DB: postgres.SQLDB{DB: db}},
		FactLoader:         postgres.NewFactStore(postgres.SQLDB{DB: db}),
		Writer:             replaceSetWriter(db),
	}
	result, err := handler.Handle(ctx, reducercontract.Intent{
		IntentID:     fmt.Sprintf("intent:11102:%d", count),
		ScopeID:      replaceSetLiveScope,
		GenerationID: replaceSetLiveGeneration,
		SourceSystem: "vulnerability_intelligence",
		Domain:       reducercontract.DomainSupplyChainImpact,
		Cause:        "synthetic #11102 owner-ceiling proof",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	var owned int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM fact_records
WHERE fact_kind = $1 AND scope_id = $2 AND generation_id = $3 AND is_tombstone = FALSE
  AND payload->>'cve_id' = $4`,
		facts.ReducerSupplyChainImpactFindingFactKind, replaceSetLiveScope, replaceSetLiveGeneration, ownerCeilingCVE,
	).Scan(&owned); err != nil {
		t.Fatalf("count owner findings: %v", err)
	}
	if err := postgres.NewSupplyChainImpactWinnersStore(postgres.SQLDB{DB: db}).
		RebuildAllWinners(ctx, replaceSetLiveNow); err != nil {
		t.Fatalf("RebuildAllWinners() error = %v", err)
	}
	var winners int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM supply_chain_impact_canonical_winners WHERE cve_id = $1`, ownerCeilingCVE,
	).Scan(&winners); err != nil {
		t.Fatalf("count winners: %v", err)
	}
	return owned, winners, result.SubSignals
}

// TestSupplyChainImpactCappedScopeOwnerHoldsEveryConsumerLive is the #11102 headline
// proof: one owner pass for one advisory stores a finding for every consuming
// repository, across the 256 boundary the QA census found pinned.
func TestSupplyChainImpactCappedScopeOwnerHoldsEveryConsumerLive(t *testing.T) {
	for _, count := range []int{255, 256, 257, 300} {
		t.Run(fmt.Sprintf("consumers=%d", count), func(t *testing.T) {
			owned, winners, signals := runOwnerCeilingPass(t, seedOwnerCeilingConsumers, count)
			if owned != count {
				t.Errorf("owner scope holds %d findings for %s, want %d (one per consuming repository); sub-signals %v",
					owned, ownerCeilingCVE, count, signals)
			}
			if winners != count {
				t.Errorf("winners holds %d keys for %s, want %d", winners, ownerCeilingCVE, count)
			}
		})
	}
}

// TestSupplyChainImpactCappedScopeOwnerHoldsEveryManifestConsumerLive is the
// manifest-arm twin of the headline proof: the consumers are git lockfile
// dependency rows (content_entity), the evidence the QA findings rest on, so a
// cut in the manifest read or its derivation past 256 fails here.
func TestSupplyChainImpactCappedScopeOwnerHoldsEveryManifestConsumerLive(t *testing.T) {
	for _, count := range []int{255, 256, 257, 300} {
		t.Run(fmt.Sprintf("consumers=%d", count), func(t *testing.T) {
			owned, winners, signals := runOwnerCeilingPass(t, seedOwnerCeilingManifestConsumers, count)
			// The pass must have read every lockfile row through the
			// manifest arm, or a green count would prove nothing about it.
			if got := signals["manifest_dependency_facts"]; got != float64(count) {
				t.Errorf("manifest_dependency_facts = %v, want %d (the manifest arm must load every consumer row)", got, count)
			}
			if owned != count {
				t.Errorf("owner scope holds %d findings for %s, want %d (one per manifest-consuming repository); sub-signals %v",
					owned, ownerCeilingCVE, count, signals)
			}
			if winners != count {
				t.Errorf("winners holds %d keys for %s, want %d", winners, ownerCeilingCVE, count)
			}
		})
	}
}
