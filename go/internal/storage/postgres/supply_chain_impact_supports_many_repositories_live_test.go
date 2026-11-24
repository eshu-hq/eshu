// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11199 many-small-repository call cost on real PostgreSQL: GetSupplyChainAdvisoriesForRepos over 200 repositories with five supports each drains one repository at a time, runs exactly one readiness statement and one page statement per repository, returns every repository's records grouped by repository, and its wall time and statement count are logged as the measured cost of that call shape
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_POSTGRES_DISPOSABLE

import (
	"fmt"
	"testing"
	"time"
)

// TestSupplyChainAdvisoriesForReposManySmallRepositoriesLive measures the call
// shape the per-repository drain makes slower than one drain: many repositories
// with few supports each. Each repository id costs one page statement, so a call
// of 200 repositories is 201 statements (one readiness probe).
func TestSupplyChainAdvisoriesForReposManySmallRepositoriesLive(t *testing.T) {
	db, ctx := openIsolatedLiveDB(t, "b2c_many_repos",
		workflowControlIntegrationDSNEnv+" is not set; skipping Postgres integration test")
	b2cSeedScopes(t, ctx, db)
	const repositories, perRepository = 200, 5
	if _, err := db.ExecContext(ctx, `
INSERT INTO supply_chain_impact_finding_supports (fact_id, canonical_key, scope_id, generation_id, finding_id, has_payload_finding_id,
  priority_score, source_confidence, suppression_state, repository_id, cve_id, package_id, payload)
SELECT md5(r::text || '-' || i::text) || md5(i::text), 'k-'||r||'-'||i, $1, $2, 'f-'||r||'-'||i, 1, 50, 'exact', 'none',
       'repo:small-'||r, 'CVE-'||i, 'pkg:'||i,
       jsonb_build_object('repository_id', 'repo:small-'||r, 'cve_id', 'CVE-'||i, 'package_name', 'p'||i, 'ecosystem', 'npm')
FROM generate_series(1, $3::int) AS r, generate_series(1, $4::int) AS i`,
		b2cScopeA, b2cGenA, repositories, perRepository); err != nil {
		t.Fatalf("seed supports: %v", err)
	}
	// The disposable database has no finding facts for the backfill to sweep, so
	// the readiness marker the production backfill would set is written directly.
	if _, err := db.ExecContext(ctx, `
INSERT INTO supply_chain_impact_finding_supports_backfill (marker_name, contract_version, completed_at)
VALUES ($1, $2, now())`, SupplyChainImpactSupportsBackfillMarker, SupplyChainImpactSupportsContractVersion); err != nil {
		t.Fatalf("write readiness marker: %v", err)
	}
	ids := make([]string, 0, repositories)
	for r := 1; r <= repositories; r++ {
		ids = append(ids, fmt.Sprintf("repo:small-%d", r))
	}
	counting := &countingExecQueryer{SQLDB: SQLDB{DB: db}}
	store := NewFactStore(counting)

	started := time.Now()
	byRepo, err := store.GetSupplyChainAdvisoriesForRepos(ctx, ids)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("GetSupplyChainAdvisoriesForRepos() error = %v, want nil", err)
	}
	if len(byRepo) != repositories {
		t.Fatalf("repositories returned = %d, want %d", len(byRepo), repositories)
	}
	for id, records := range byRepo {
		if len(records) != perRepository {
			t.Fatalf("repository %s records = %d, want %d", id, len(records), perRepository)
		}
	}
	if want := 1 + repositories; counting.statements != want {
		t.Fatalf("statements = %d, want %d (one readiness probe and one page per repository)", counting.statements, want)
	}
	t.Logf("%d repositories x %d supports: %d statements, %s wall time (%.2f ms a repository)",
		repositories, perRepository, counting.statements, elapsed.Round(time.Millisecond),
		float64(elapsed.Microseconds())/1000/float64(repositories))
}
