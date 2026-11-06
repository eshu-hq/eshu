// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"encoding/json"
	"slices"
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

// goFenceWinnersForCVE rebuilds the canonical winners and returns the API rows
// for one CVE id, so the eshu-hq/eshu#10095 verdict assertions read exactly
// what the API serves.
func goFenceWinnersForCVE(c *convergence, cveID string) []query.SupplyChainImpactFindingRow {
	c.t.Helper()
	if err := (postgres.SupplyChainImpactWinnersStore{DB: postgres.SQLDB{DB: c.db}}).RebuildAllWinners(c.ctx, time.Now().UTC()); err != nil {
		c.t.Fatalf("rebuild winners: %v", err)
	}
	rows, err := query.NewPostgresSupplyChainImpactFindingStoreWithReadModel(c.db, true).ListSupplyChainImpactFindings(c.ctx,
		query.SupplyChainImpactFindingFilter{CVEID: cveID, IncludeSuppressed: true, Limit: 10})
	if err != nil {
		c.t.Fatalf("list findings: %v", err)
	}
	return rows
}

// goFenceWinnersTableCount counts the canonical winners rows for one CVE id:
// canonical_key is the table's primary key, so this is the number of distinct
// canonical keys the winners hold for the advisory.
func goFenceWinnersTableCount(c *convergence, cveID string) int {
	c.t.Helper()
	var count int
	if err := c.db.QueryRowContext(c.ctx, `SELECT count(*) FROM supply_chain_impact_canonical_winners WHERE cve_id = $1`, cveID).Scan(&count); err != nil {
		c.t.Fatalf("count winners rows for %s: %v", cveID, err)
	}
	return count
}

// assertGoFenceWinnerVerdict requires the API to serve exactly the canonical
// winners rows for the CVE (one winners row per canonical key) and every one
// of them to carry the govulncheck verdict: the runtime detail wantRuntime
// and the reachability envelope state wantState. With Go evidence the holder
// reads not_called/not_called; without it the exact-match decision drops back
// to package_manifest with an unknown envelope state, the pre-#10095 truth.
func assertGoFenceWinnerVerdict(c *convergence, cveID, wantRuntime, wantState string) {
	c.t.Helper()
	rows := goFenceWinnersForCVE(c, cveID)
	if got := goFenceWinnersTableCount(c, cveID); got != len(rows) {
		c.t.Fatalf("API rows for %s = %d, want the %d canonical winners rows (one winners row per canonical key)", cveID, len(rows), got)
	}
	if len(rows) == 0 {
		c.t.Fatalf("no winners rows for %s", cveID)
	}
	for _, row := range rows {
		if row.RuntimeReachability != wantRuntime {
			c.t.Fatalf("winner %s runtime reachability = %q, want %q (rows %+v)", row.FindingID, row.RuntimeReachability, wantRuntime, rows)
		}
		if row.Reachability == nil || row.Reachability.State != wantState || row.Reachability.Source != "govulncheck" {
			c.t.Fatalf("winner %s reachability block = %+v, want state %q source govulncheck", row.FindingID, row.Reachability, wantState)
		}
	}
}

// TestGoFenceReopensAHolderAndItsReducerPassRunsLive is the chain proof of ruling Amendment
// 3 over the real handler, writer, fan-out store, and queue, plus the eshu-hq/eshu#10095
// verdict flip: with Go evidence active the holder's winners read not_called with the
// govulncheck source; after the fence retracts the go-reachability generation and the
// reopened holder re-runs, the winners drop back to unknown. The seeded Go-scope item
// stands in for the trigger of eshu-hq/eshu#10077.
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
	assertGoFenceWinnerVerdict(c, goFenceAdvisory, string(supplychaincore.GoVulnReachabilityNotCalled), "not_called")

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
	assertGoFenceWinnerVerdict(c, goFenceAdvisory, "package_manifest", "unknown")
}

const (
	goFenceAliasCVE        = "CVE-2026-10095"
	goFenceAliasAdvisory   = "GO-2026-10095"
	goFenceAliasModule     = "example.com/alias"
	goFenceAliasPackage    = "gomod://proxy.golang.org/example.com/alias"
	goFenceAliasRepository = "repository:r_a10095a1"
	goFenceAliasHolder     = "scope:10095:alias"
	goFenceAliasScope      = "go-reachability://repository:r_a10095a1"
)

// TestGoFenceAliasCopyReadsNotCalledLive is the eshu-hq/eshu#10095 alias case:
// the advisory joins on the GO id while the finding is keyed by its CVE
// alias, and the alias copy still reads the govulncheck not_called verdict
// through the cross-scope reader.
func TestGoFenceAliasCopyReadsNotCalledLive(t *testing.T) {
	c := newConvergence(t, false)
	fanOutSeedScope(c.t, c.ctx, c.db, goFenceAliasHolder, "g1")
	holderGeneration := fanOutGen(goFenceAliasHolder, "g1")
	c.upsert(
		c.envelope(goFenceAliasHolder, holderGeneration, "fact:10095:alias-cve", chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": goFenceAliasAdvisory, "cve_id": goFenceAliasCVE, "source": "osv", "cvss_score": 7.5,
			"aliases": []any{goFenceAliasCVE, goFenceAliasAdvisory},
		}),
		c.envelope(goFenceAliasHolder, holderGeneration, "fact:10095:alias-affected", chain.VulnerabilityAffectedPackageFactKind, map[string]any{
			"advisory_id": goFenceAliasAdvisory, "cve_id": goFenceAliasCVE, "package_id": goFenceAliasPackage, "ecosystem": "gomod",
			"package_name": goFenceAliasModule, "purl": "pkg:golang/" + goFenceAliasModule, "fixed_versions": []any{"1.2.0"},
			"affected_ranges": []any{map[string]any{"type": "SEMVER", "events": []any{
				map[string]any{"introduced": "0"}, map[string]any{"fixed": "1.2.0"},
			}}},
		}),
		c.envelope(goFenceAliasHolder, holderGeneration, "fact:10095:alias-consumption", factschema.FactKindReducerPackageConsumptionCorrelation, map[string]any{
			"package_id": goFenceAliasPackage, "relationship_kind": "consumption", "repository_id": goFenceAliasRepository,
			"dependency_range": "v1.1.0", "canonical_writes": 1, "evidence_fact_ids": []string{"fact:10095:alias-manifest"},
		}),
	)
	fanOutSeedItem(c.t, c.ctx, c.db, goFenceAliasHolder, "g1")

	fanOutSeedScope(c.t, c.ctx, c.db, goFenceAliasScope, "g1")
	aliasGeneration := fanOutGen(goFenceAliasScope, "g1")
	c.upsert(
		c.envelope(goFenceAliasScope, aliasGeneration, "fact:10095:alias-module", chain.VulnerabilityGoModuleEvidenceFactKind, map[string]any{
			"repository_id": goFenceAliasRepository, "relative_path": "go.mod", "module_path": goFenceAliasModule,
			"required_version": "v1.1.0", "indirect": false, "replacement_path": "", "replacement_version": "",
			"ecosystem": "go-module", "package_id": "go-module/proxy.golang.org/" + goFenceAliasModule,
			"package_name": goFenceAliasModule, "line_number": 5,
		}),
		c.envelope(goFenceAliasScope, aliasGeneration, "fact:10095:alias-reach", chain.VulnerabilityGoCallReachabilityFactKind, map[string]any{
			"repository_id": goFenceAliasRepository, "osv_id": goFenceAliasAdvisory, "deepest_module": goFenceAliasModule,
			"deepest_package": "", "deepest_symbol": "", "reachability_level": "not_called",
			"trace": []map[string]string{{"module": goFenceAliasModule, "version": "v1.1.0"}},
		}),
	)
	fanOutSeedItem(c.t, c.ctx, c.db, goFenceAliasScope, "g1")
	c.drain()
	assertGoFenceWinnerVerdict(c, goFenceAliasCVE, string(supplychaincore.GoVulnReachabilityNotCalled), "not_called")
}

// TestGoFenceReaderLoadsActiveGoEvidenceLive is the eshu-hq/eshu#10095
// probe-level proof for FactStore.ListActiveGoReachabilityFacts: on a seeded
// matrix it returns exactly the live Go evidence of the requested
// repositories' active generations (a tombstoned twin, a non-Go kind, and a
// superseded generation's fact stay out), and empty, non-canonical, and
// unscanned repository ids load nothing.
func TestGoFenceReaderLoadsActiveGoEvidenceLive(t *testing.T) {
	c := newConvergence(t, false)
	store := postgres.NewFactStore(postgres.SQLDB{DB: c.db})

	const (
		repoA = "repository:r_b10095b1"
		repoB = "repository:r_b10095b2"
	)
	const (
		scopeA = "go-reachability://repository:r_b10095b1"
		scopeB = "go-reachability://repository:r_b10095b2"
	)
	fanOutSeedScope(c.t, c.ctx, c.db, scopeA, "g1", "g0")
	fanOutSeedScope(c.t, c.ctx, c.db, scopeB, "g1")
	genA, oldA, genB := fanOutGen(scopeA, "g1"), fanOutGen(scopeA, "g0"), fanOutGen(scopeB, "g1")
	readerEnvelope := func(scope, generation, id, kind string, payload map[string]any) facts.Envelope {
		return facts.Envelope{
			FactID: id, ScopeID: scope, GenerationID: generation, FactKind: kind, StableFactKey: id,
			SchemaVersion: chain.VulnerabilityIntelligenceSchemaVersionV1, CollectorKind: "synthetic",
			SourceConfidence: facts.SourceConfidenceReported, ObservedAt: time.Date(2026, 10, 31, 11, 0, 0, 0, time.UTC),
			Payload:   payload,
			SourceRef: facts.Ref{SourceSystem: "synthetic", ScopeID: scope, GenerationID: generation, FactKey: id},
		}
	}
	readerModule := func(repository string) map[string]any {
		return map[string]any{
			"repository_id": repository, "relative_path": "go.mod", "module_path": "example.com/reader",
			"required_version": "v1.1.0", "indirect": false, "replacement_path": "", "replacement_version": "",
			"ecosystem": "go-module", "package_id": "go-module/proxy.golang.org/example.com/reader",
			"package_name": "example.com/reader", "line_number": 5,
		}
	}
	readerReach := func(repository, osvID string) map[string]any {
		return map[string]any{
			"repository_id": repository, "osv_id": osvID, "deepest_module": "example.com/reader",
			"deepest_package": "", "deepest_symbol": "", "reachability_level": "not_called",
			"trace": []map[string]string{{"module": "example.com/reader", "version": "v1.1.0"}},
		}
	}
	tombstoned := readerEnvelope(scopeA, genA, "fact:reader:reach-tomb", chain.VulnerabilityGoCallReachabilityFactKind,
		readerReach(repoA, "GO-2026-10095"))
	tombstoned.IsTombstone = true
	c.upsert(
		readerEnvelope(scopeA, genA, "fact:reader:module", chain.VulnerabilityGoModuleEvidenceFactKind, readerModule(repoA)),
		readerEnvelope(scopeA, genA, "fact:reader:reach", chain.VulnerabilityGoCallReachabilityFactKind, readerReach(repoA, "GO-2026-10095")),
		tombstoned,
		readerEnvelope(scopeA, genA, "fact:reader:cve", chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": "GO-2026-10095", "cve_id": "GO-2026-10095",
		}),
		readerEnvelope(scopeA, oldA, "fact:reader:reach-old", chain.VulnerabilityGoCallReachabilityFactKind,
			readerReach(repoA, "GO-2026-10095")),
		readerEnvelope(scopeB, genB, "fact:reader:b-reach", chain.VulnerabilityGoCallReachabilityFactKind,
			readerReach(repoB, "GO-2026-10095")),
	)

	load := func(repositories ...string) []string {
		loaded, err := store.ListActiveGoReachabilityFacts(c.ctx, supplychaincore.GoReachabilityFactFilter{RepositoryIDs: repositories})
		if err != nil {
			c.t.Fatalf("ListActiveGoReachabilityFacts(%q): %v", repositories, err)
		}
		var out []string
		for _, envelope := range loaded {
			out = append(out, envelope.FactID)
		}
		slices.Sort(out)
		return out
	}
	if got, want := load(repoA), []string{"fact:reader:module", "fact:reader:reach"}; !slices.Equal(got, want) {
		t.Fatalf("repository A loads %q, want %q (tombstoned twin, non-Go kind, and superseded generation stay out)", got, want)
	}
	if got, want := load(repoA, repoB), []string{"fact:reader:b-reach", "fact:reader:module", "fact:reader:reach"}; !slices.Equal(got, want) {
		t.Fatalf("both repositories load %q, want %q", got, want)
	}
	if got := load(); len(got) != 0 {
		t.Fatalf("empty filter loads %q, want nothing", got)
	}
	if got := load("repository:r_SHORT", "https://example.com/owner/repo", "repo://owner/repo"); len(got) != 0 {
		t.Fatalf("non-canonical ids load %q, want nothing", got)
	}
	if got := load("repository:r_ffffffff"); len(got) != 0 {
		t.Fatalf("an unscanned canonical id loads %q, want nothing", got)
	}
}
