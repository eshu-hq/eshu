// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"encoding/json"
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
// active generation says the vulnerable surface is not called.
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

// holderReachability reads the holder's active copy of the advisory's finding.
func holderReachability(c *convergence) string {
	c.t.Helper()
	var reachability string
	if err := c.db.QueryRowContext(c.ctx, `
SELECT COALESCE(f.payload->>'runtime_reachability', '')
FROM fact_records f
JOIN ingestion_scopes s ON s.scope_id = f.scope_id AND s.active_generation_id = f.generation_id
WHERE f.fact_kind = $1 AND NOT f.is_tombstone AND f.scope_id = $2 AND f.payload->>'cve_id' = $3`,
		facts.ReducerSupplyChainImpactFindingFactKind, goFenceHolder, goFenceAdvisory).Scan(&reachability); err != nil {
		c.t.Fatalf("read the holder's finding: %v (handled %v)", err, c.handled)
	}
	return reachability
}

// TestGoFenceReopensAHolderSoItsFindingDropsNotCalledLive is the chain proof of ruling
// Amendment 2, A2.6, with the real handler, writer, fan-out store, queue, and winners. A
// holder's finding reads not_called. The fence reopens the holder in its own transaction,
// with the origin stamp. One reducer pass reads no Go evidence and rewrites the copy, and
// the API finding through the winners no longer reads not_called. Without the reopen the
// holder's item stays succeeded and the verdict stays, with no bound.
func TestGoFenceReopensAHolderSoItsFindingDropsNotCalledLive(t *testing.T) {
	c := newConvergence(t, false)
	holderItem, generationID := seedGoFenceChain(c)
	c.drain()

	if got := holderReachability(c); got != string(supplychaincore.GoVulnReachabilityNotCalled) {
		t.Fatalf("holder reachability before the fence = %q, want not_called: the seed does not reach the finding (handled %v)", got, c.handled)
	}

	store := gomodules.NewStore(postgres.SQLDB{DB: c.db}).WithHolderReopener(postgres.ReopenSupplyChainImpactHolderInTransaction)
	fence := []scannercomponent.StaleEvidenceFence{{ScopeID: goFenceScope, GenerationID: generationID}}
	result, err := store.FenceStaleGoEvidence(c.ctx, fence, time.Now().UTC())
	if err != nil || result.Fenced != 1 || result.HoldersReopened != 1 {
		t.Fatalf("fence result = %+v, err = %v, want 1 fenced and 1 holder", result, err)
	}

	var status string
	var origin json.RawMessage
	if err := c.db.QueryRowContext(c.ctx,
		`SELECT status, payload->'`+supplychaincore.PayloadKeyFanOutOrigin+`' FROM fact_work_items WHERE work_item_id = $1`,
		holderItem).Scan(&status, &origin); err != nil {
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
	got := holderReachability(c)
	if got == string(supplychaincore.GoVulnReachabilityNotCalled) {
		t.Fatalf("holder reachability after one reducer pass = %q, want the stale verdict gone (handled %v)", got, c.handled)
	}
	t.Logf("holder reachability after the fence and one reducer pass: %q", got)

	if err := (postgres.SupplyChainImpactWinnersStore{DB: postgres.SQLDB{DB: c.db}}).RebuildAllWinners(c.ctx, time.Now().UTC()); err != nil {
		t.Fatalf("rebuild winners: %v", err)
	}
	rows, err := query.NewPostgresSupplyChainImpactFindingStoreWithReadModel(c.db, true).ListSupplyChainImpactFindings(c.ctx,
		query.SupplyChainImpactFindingFilter{CVEID: goFenceAdvisory, IncludeSuppressed: true, Limit: 10})
	if err != nil {
		t.Fatalf("list findings: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("API findings = %d, want exactly one canonical finding", len(rows))
	}
	if rows[0].RuntimeReachability == string(supplychaincore.GoVulnReachabilityNotCalled) {
		t.Fatalf("API finding reachability = %q, want the stale verdict gone", rows[0].RuntimeReachability)
	}
}

// TestGoFenceWithoutAReopenLeavesTheVerdictStaleLive is the sensitivity witness for A2.4:
// a retraction alone changes active facts and nothing re-derives. Keep it next to the
// chain proof, so a reader sees what the reopen is for.
func TestGoFenceWithoutAReopenLeavesTheVerdictStaleLive(t *testing.T) {
	c := newConvergence(t, false)
	_, generationID := seedGoFenceChain(c)
	c.drain()
	if _, err := c.db.ExecContext(c.ctx, `
UPDATE ingestion_scopes SET active_generation_id = NULL WHERE scope_id = $1 AND active_generation_id = $2`, goFenceScope, generationID); err != nil {
		t.Fatalf("detach: %v", err)
	}
	if _, err := c.db.ExecContext(c.ctx, `
UPDATE scope_generations SET status = 'superseded', superseded_at = now() WHERE generation_id = $1`, generationID); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	c.drain()
	if got := holderReachability(c); got != string(supplychaincore.GoVulnReachabilityNotCalled) {
		t.Fatalf("holder reachability = %q, want it still not_called: this witness shows why the reopen exists", got)
	}
}
