// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu-sdk/factschema"
	"github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/query"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/gomodules"
)

const (
	goFenceAdvisory   = "GO-2026-10055"
	goFenceModule     = "example.com/fence"
	goFencePackage    = "gomod://proxy.golang.org/example.com/fence"
	goFenceRepository = "repository:r_0001a055"
	goFenceHolder     = "scope:10055:osv"
	goFenceScope      = "go-reachability://" + goFenceRepository
)

// seedGoFenceChain writes a Go advisory's holder scope and a go-reachability scope whose
// active generation says the vulnerable surface is not called. BuildSupplyChainImpactFindings
// reads not_called from these facts when they are in hand, so this seed is the one
// eshu-hq/eshu#10095's acceptance test reuses.
func seedGoFenceChain(c *convergence) (holderItem, generationID string) {
	c.t.Helper()
	fanOutSeedScope(c.t, c.ctx, c.db, goFenceHolder, "g1")
	holderGeneration := fanOutGen(goFenceHolder, "g1")
	c.upsert(
		c.envelope(goFenceHolder, holderGeneration, "fact:10055:cve", chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": goFenceAdvisory, "cve_id": goFenceAdvisory, "source": "osv", "cvss_score": 7.5,
			"aliases": []any{goFenceAdvisory},
		}),
		c.envelope(goFenceHolder, holderGeneration, "fact:10055:affected", chain.VulnerabilityAffectedPackageFactKind, map[string]any{
			"advisory_id": goFenceAdvisory, "cve_id": goFenceAdvisory, "package_id": goFencePackage, "ecosystem": "gomod",
			"package_name": goFenceModule, "purl": "pkg:golang/" + goFenceModule, "fixed_versions": []any{"1.2.0"},
			"affected_ranges": []any{map[string]any{"type": "SEMVER", "events": []any{
				map[string]any{"introduced": "0"}, map[string]any{"fixed": "1.2.0"},
			}}},
		}),
		c.envelope(goFenceHolder, holderGeneration, "fact:10055:consumption", factschema.FactKindReducerPackageConsumptionCorrelation, map[string]any{
			"package_id": goFencePackage, "relationship_kind": "consumption", "repository_id": goFenceRepository,
			"dependency_range": "v1.1.0", "canonical_writes": 1, "evidence_fact_ids": []string{"fact:10055:manifest"},
		}),
	)
	holderItem = fanOutSeedItem(c.t, c.ctx, c.db, goFenceHolder, "g1")

	fanOutSeedScope(c.t, c.ctx, c.db, goFenceScope, "g1")
	generationID = fanOutGen(goFenceScope, "g1")
	c.upsert(
		c.envelope(goFenceScope, generationID, "fact:10055:module", chain.VulnerabilityGoModuleEvidenceFactKind, map[string]any{
			"repository_id": goFenceRepository, "relative_path": "go.mod", "module_path": goFenceModule,
			"required_version": "v1.1.0", "indirect": false, "replacement_path": "", "replacement_version": "",
			"ecosystem": "go-module", "package_id": "go-module/proxy.golang.org/" + goFenceModule,
			"package_name": goFenceModule, "line_number": 5,
		}),
		c.envelope(goFenceScope, generationID, "fact:10055:reach", chain.VulnerabilityGoCallReachabilityFactKind, map[string]any{
			"repository_id": goFenceRepository, "osv_id": goFenceAdvisory, "deepest_module": goFenceModule,
			"deepest_package": "", "deepest_symbol": "", "reachability_level": "not_called",
			"trace": []map[string]string{{"module": goFenceModule, "version": "v1.1.0"}},
		}),
	)
	return holderItem, generationID
}

// activeCopies counts one scope's active copies of the advisory's finding.
func activeCopies(c *convergence, scopeID string) int {
	c.t.Helper()
	var count int
	if err := c.db.QueryRowContext(c.ctx, `
SELECT count(*)
FROM fact_records f
JOIN ingestion_scopes s ON s.scope_id = f.scope_id AND s.active_generation_id = f.generation_id
WHERE f.fact_kind = $1 AND NOT f.is_tombstone AND f.scope_id = $2 AND f.payload->>'cve_id' = $3`,
		facts.ReducerSupplyChainImpactFindingFactKind, scopeID, goFenceAdvisory).Scan(&count); err != nil {
		c.t.Fatalf("count the findings of %s: %v", scopeID, err)
	}
	return count
}

// logAPIReachability records what the API reads through the winners. It is an observation
// only: asserting it would break when eshu-hq/eshu#10095 lands.
func logAPIReachability(c *convergence, when string) {
	c.t.Helper()
	if err := (postgres.SupplyChainImpactWinnersStore{DB: postgres.SQLDB{DB: c.db}}).RebuildAllWinners(c.ctx, time.Now().UTC()); err != nil {
		c.t.Fatalf("rebuild winners: %v", err)
	}
	rows, err := query.NewPostgresSupplyChainImpactFindingStoreWithReadModel(c.db, true).ListSupplyChainImpactFindings(c.ctx,
		query.SupplyChainImpactFindingFilter{CVEID: goFenceAdvisory, IncludeSuppressed: true, Limit: 10})
	if err != nil {
		c.t.Fatalf("list findings: %v", err)
	}
	reachability := make([]string, 0, len(rows))
	for _, row := range rows {
		reachability = append(reachability, row.RuntimeReachability)
	}
	c.t.Logf("API reachability %s: %q", when, reachability)
}

// TestGoFenceReopensAHolderAndItsReducerPassRunsLive is the chain proof of ruling Amendment
// 3 over the real handler, writer, fan-out store, and queue. It asserts only what holds with
// or without eshu-hq/eshu#10095: after the fence the go-reachability scope's copy is out of
// the active set, a holder of the repository's findings is reopened in the fence's
// transaction with the origin stamp, and one reducer pass runs for that holder. The seeded
// Go-scope item stands in for the trigger of eshu-hq/eshu#10077.
//
// It asserts no verdict. The API's value is logged only.
func TestGoFenceReopensAHolderAndItsReducerPassRunsLive(t *testing.T) {
	c := newConvergence(t, false)
	holderItem, generationID := seedGoFenceChain(c)
	fanOutSeedItem(c.t, c.ctx, c.db, goFenceScope, "g1")
	c.drain()
	passes := func() int {
		count := 0
		for _, handled := range c.handled {
			if strings.HasPrefix(handled, goFenceHolder+"#") {
				count++
			}
		}
		return count
	}
	before := passes()
	if before == 0 {
		t.Fatalf("the holder never ran before the fence (handled %v)", c.handled)
	}
	if got := activeCopies(c, goFenceScope); got != 1 {
		t.Fatalf("active Go scope copies before the fence = %d, want 1 (handled %v)", got, c.handled)
	}
	logAPIReachability(c, "before the fence")

	store := gomodules.NewStore(postgres.SQLDB{DB: c.db}).WithHolderReopener(postgres.ReopenSupplyChainImpactHolderInTransaction)
	fence := []scannercomponent.StaleEvidenceFence{{ScopeID: goFenceScope, GenerationID: generationID}}
	result, err := store.FenceStaleGoEvidence(c.ctx, fence, time.Now().UTC())
	if err != nil || result.Fenced != 1 || result.HoldersReopened != 1 {
		t.Fatalf("fence result = %+v, err = %v, want 1 fenced and 1 holder", result, err)
	}
	if got := activeCopies(c, goFenceScope); got != 0 {
		t.Fatalf("active Go scope copies after the fence = %d, want 0", got)
	}

	var status string
	var origin json.RawMessage
	readItem := `SELECT status, COALESCE(payload->'` + supplychaincore.PayloadKeyFanOutOrigin + `', 'null'::jsonb) FROM fact_work_items WHERE work_item_id = $1`
	if err := c.db.QueryRowContext(c.ctx, readItem, holderItem).Scan(&status, &origin); err != nil {
		t.Fatalf("read the holder item: %v", err)
	}
	var stamp map[string]string
	if err := json.Unmarshal(origin, &stamp); err != nil {
		t.Fatalf("decode origin %s: %v", origin, err)
	}
	if status != "pending" || stamp["scope_id"] != goFenceScope || stamp["generation_id"] != generationID {
		t.Fatalf("holder item = %q with origin %v, want pending and stamped with the fenced generation", status, stamp)
	}

	c.drain()
	if after := passes(); after != before+1 {
		t.Fatalf("holder passes = %d after the fence, want %d (handled %v)", after, before+1, c.handled)
	}
	logAPIReachability(c, "after the fence and the holder's pass")
}
