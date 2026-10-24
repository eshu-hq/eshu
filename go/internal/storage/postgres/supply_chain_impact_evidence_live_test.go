// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8880 disposable PostgreSQL 18 proofs that the supply-chain impact active-evidence loader returns byte-identical rows, order, and truncation to the frozen pre-#8880 statement across the arm matrix, and spends at least 100x fewer shared buffers than it on a scaled TOASTed file corpus under both plan_cache_mode values; wall time is logged, not gated
//eshu:readiness:env ESHU_SUPPLY_CHAIN_IMPACT_EVIDENCE_PROOF_DISPOSABLE ESHU_SUPPLY_CHAIN_IMPACT_EVIDENCE_PROOF_DSN

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestSupplyChainImpactActiveEvidenceMatchesRetiredStatementLive is the #8880
// differential (ruling 4.2). For every filter shape the production loader must
// return exactly the rows the frozen retired statement returns, in the same
// (core by byte order, identity, suppression by byte order) order, with the
// same truncation bool, duplicate-free.
func TestSupplyChainImpactActiveEvidenceMatchesRetiredStatementLive(t *testing.T) {
	ctx, database := openSupplyChainImpactEvidenceProofDB(t)
	seedSupplyChainImpactMatrix(t, ctx, database)
	recorder := &supplyChainImpactRecordingDB{SQLDB: SQLDB{DB: database}}

	all := supplyChainImpactMatrixAllFilter()
	for _, tc := range []struct {
		name          string
		filter        supplychaincore.SupplyChainImpactFactFilter
		suppressCap   int
		wantTruncated bool
		wantPresent   []string
		wantAbsent    []string
	}{
		{
			name: "npm first round",
			filter: supplychaincore.SupplyChainImpactFactFilter{
				PackageIDs: []string{scimPackage}, PURLs: []string{scimPURL},
				CVEIDs: []string{scimCVE}, AdvisoryIDs: []string{scimAdvisory},
				FileRepositoryIDs: scimRepositories,
			},
			wantPresent: []string{"m:ap:two-keys", "m:file:r1:ts", "m:file:r2:scope-only", "m:file:r5:foreign-cve", "m:cve:foreign-package", "m:supp:pkg-unicode", "m:supp:purl-pad", "m:supp:cve-lower", "m:supp:adv"},
			wantAbsent:  []string{"m:ap:tombstone", "m:ap:old-generation", "m:ap:pending", "m:file:r1:go", "m:file:r5:ts", "m:supp:deploy-only"},
		},
		{
			name:        "npm follow-up with every identity",
			filter:      all,
			wantPresent: []string{"m:ci:r4:source-key", "m:svc:r3:scope-id", "m:wl:r2:payload-id", "m:plat:r4:image-ref", "m:cons:r3:payload-scope", "m:oci:manifest:foreign-image", "m:supp:repo", "m:supp:subject"},
			wantAbsent:  []string{"m:ci:r5", "m:supp:deploy-only"},
		},
		{name: "kev cve only", filter: supplychaincore.SupplyChainImpactFactFilter{CVEIDs: []string{scimCVE}}, wantAbsent: []string{"m:file:r5:foreign-cve"}},
		{name: "maven purl and cve without files", filter: supplychaincore.SupplyChainImpactFactFilter{PURLs: []string{"pkg:maven/org.example/lib@1.0.0"}, CVEIDs: []string{scimCVE}}},
		{name: "repository follow-up only", filter: supplychaincore.SupplyChainImpactFactFilter{RepositoryIDs: scimRepositories}},
		{name: "digest only", filter: supplychaincore.SupplyChainImpactFactFilter{SubjectDigests: []string{scimDigest}}},
		{name: "suppression cap crosses mid-stream", filter: supplychaincore.SupplyChainImpactFactFilter{CVEIDs: []string{scimCVE}}, suppressCap: 50, wantTruncated: true},
		{name: "suppression cap exactly reached", filter: supplychaincore.SupplyChainImpactFactFilter{CVEIDs: []string{scimCVE}}, suppressCap: scimCVESuppressions},
		{name: "empty filter"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			originalCap := maxSupplyChainImpactActiveEvidenceRowsPerCall
			t.Cleanup(func() { maxSupplyChainImpactActiveEvidenceRowsPerCall = originalCap })
			if tc.suppressCap > 0 {
				maxSupplyChainImpactActiveEvidenceRowsPerCall = tc.suppressCap
			}
			want, _ := walkSupplyChainImpactRetiredOracle(t, ctx, database, tc.filter)
			got, _ := supplyChainImpactProductionRows(t, ctx, recorder, tc.filter)
			wantLines, gotLines := supplyChainImpactRowLines(t, want), supplyChainImpactRowLines(t, got)
			if !reflect.DeepEqual(gotLines, wantLines) {
				t.Fatalf("production loader diverged from the retired statement\n%s", supplyChainImpactLineDiff(wantLines, gotLines))
			}
			if got.Truncated != tc.wantTruncated {
				t.Fatalf("truncated = %v, want %v", got.Truncated, tc.wantTruncated)
			}
			ids := map[string]bool{}
			for _, id := range sortedSupplyChainImpactFactIDs(got) {
				ids[id] = true
			}
			for _, id := range tc.wantPresent {
				if !ids[id] {
					t.Errorf("fixture row %s missing from both answers: the matrix no longer exercises its arm", id)
				}
			}
			for _, id := range tc.wantAbsent {
				if ids[id] {
					t.Errorf("fixture row %s must not be selected", id)
				}
			}
		})
	}
}

// TestSupplyChainImpactActiveEvidenceBufferBudgetLive is the #8880 plan and
// budget proof (ruling 4.1e on the scaled fixture). It replays every statement
// the production loader sends for one call, and every page of the frozen
// retired statement for the same filter, under PREPARE + EXPLAIN (ANALYZE,
// BUFFERS) EXECUTE in both plan_cache_mode values, and requires the production
// call to touch at least 100x fewer shared buffers for the npm shape (the
// TOASTed file corpus the retired statement walked on every page).
func TestSupplyChainImpactActiveEvidenceBufferBudgetLive(t *testing.T) {
	ctx, database := openSupplyChainImpactEvidenceProofDB(t)
	const repoScopes, filesPerScope, kevRows = 300, 40, 1500
	seedStart := time.Now()
	seedSupplyChainImpactScaledCorpus(t, ctx, database, repoScopes, filesPerScope, kevRows)
	t.Logf("seeded %d scopes x %d active + %d superseded TOASTed files, %d vulnerability rows per kind in %s",
		repoScopes, filesPerScope, filesPerScope, kevRows, time.Since(seedStart).Round(time.Millisecond))
	recorder := &supplyChainImpactRecordingDB{SQLDB: SQLDB{DB: database}}

	var npmRepos, npmCVEs, npmAdvisories, kevCVEs []string
	for n := 1; n <= 8; n++ {
		npmRepos = append(npmRepos, supplyChainImpactScaledRepositoryID(n*7))
	}
	for n := 1; n <= kevRows; n++ {
		cve := fmt.Sprintf("CVE-2026-%05d", n)
		kevCVEs = append(kevCVEs, cve)
		if n%40 == 1 && len(npmCVEs) < 50 {
			npmCVEs = append(npmCVEs, cve)
			npmAdvisories = append(npmAdvisories, fmt.Sprintf("GHSA-scaled-%d", n))
		}
	}
	for _, tc := range []struct {
		name     string
		filter   supplychaincore.SupplyChainImpactFactFilter
		minRatio int64
	}{
		{
			name: "npm with file repositories",
			filter: supplychaincore.SupplyChainImpactFactFilter{
				PackageIDs: []string{"npm://registry.npmjs.org/pkg-1"}, PURLs: []string{"pkg:npm/pkg-1"},
				CVEIDs: npmCVEs, AdvisoryIDs: npmAdvisories, FileRepositoryIDs: npmRepos,
			},
			minRatio: 100,
		},
		{name: "kev cve list", filter: supplychaincore.SupplyChainImpactFactFilter{CVEIDs: kevCVEs}, minRatio: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want, oracleStatements := walkSupplyChainImpactRetiredOracle(t, ctx, database, tc.filter)
			callStart := time.Now()
			got, productionStatements := supplyChainImpactProductionRows(t, ctx, recorder, tc.filter)
			callElapsed := time.Since(callStart)
			if !reflect.DeepEqual(supplyChainImpactRowLines(t, got), supplyChainImpactRowLines(t, want)) {
				t.Fatalf("production rows diverged from the retired statement on the scaled corpus (%d vs %d rows)", len(got.Rows), len(want.Rows))
			}
			for _, mode := range []string{"force_generic_plan", "force_custom_plan"} {
				baseline, _ := supplyChainImpactStatementBuffers(t, ctx, database, oracleStatements, mode)
				candidate, _ := supplyChainImpactStatementBuffers(t, ctx, database, productionStatements, mode)
				t.Logf("%s %s: rows=%d baseline buffers=%d over %d statements, production buffers=%d over %d statements (%.1fx), production call %s",
					tc.name, mode, len(got.Rows), baseline, len(oracleStatements), candidate, len(productionStatements),
					float64(baseline)/float64(max(candidate, 1)), callElapsed.Round(time.Microsecond))
				if candidate*tc.minRatio > baseline {
					t.Errorf("%s: production buffers %d x %d > baseline %d: a branch still walks the active corpus instead of probing its anchor",
						mode, candidate, tc.minRatio, baseline)
				}
			}
		})
	}
}

// openSupplyChainImpactEvidenceProofDB creates a disposable database with the
// full bootstrap schema (every migration, so every index the loader plans on
// comes from the migration files) and replaces the identity support function
// with a deterministic stub over supply_chain_impact_identity_stub.
func openSupplyChainImpactEvidenceProofDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	ctx, database := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_SUPPLY_CHAIN_IMPACT_EVIDENCE_PROOF_DSN"),
		os.Getenv("ESHU_SUPPLY_CHAIN_IMPACT_EVIDENCE_PROOF_DISPOSABLE"),
		10*time.Minute,
	)
	if err := ApplyBootstrap(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(supplyChainImpactRetiredOracleQuery))); got != supplyChainImpactRetiredOracleHash {
		t.Fatalf("frozen retired oracle hash = %s, want %s", got, supplyChainImpactRetiredOracleHash)
	}
	if _, err := database.ExecContext(ctx, `
CREATE TABLE supply_chain_impact_identity_stub (LIKE fact_records);
CREATE OR REPLACE FUNCTION container_image_identity_current_support_facts_for(
    digests TEXT[], image_refs TEXT[], repository_ids TEXT[],
    source_repository_ids TEXT[], scope_ids TEXT[], after_fact_id TEXT,
    result_limit INTEGER
)
RETURNS TABLE (
    fact_id TEXT, scope_id TEXT, generation_id TEXT, fact_kind TEXT,
    stable_fact_key TEXT, schema_version TEXT, collector_kind TEXT,
    fencing_token BIGINT, source_confidence TEXT, source_system TEXT,
    source_fact_key TEXT, source_uri TEXT, source_record_id TEXT,
    observed_at TIMESTAMPTZ, is_tombstone BOOLEAN, payload JSONB
)
LANGUAGE sql
STABLE
AS $function$
    SELECT stub.fact_id, stub.scope_id, stub.generation_id, stub.fact_kind,
           stub.stable_fact_key, stub.schema_version, stub.collector_kind,
           stub.fencing_token, stub.source_confidence, stub.source_system,
           stub.source_fact_key, COALESCE(stub.source_uri, ''),
           COALESCE(stub.source_record_id, ''), stub.observed_at,
           stub.is_tombstone, stub.payload
    FROM supply_chain_impact_identity_stub AS stub
    WHERE $6 = '' OR convert_to(stub.fact_id, 'UTF8') > convert_to($6, 'UTF8')
    ORDER BY convert_to(stub.fact_id, 'UTF8')
    LIMIT $7
$function$;`); err != nil {
		t.Fatalf("install identity stub: %v", err)
	}
	return ctx, database
}

// supplyChainImpactLineDiff prints the first divergence and both lengths.
func supplyChainImpactLineDiff(want, got []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "want %d lines, got %d lines\n", len(want), len(got))
	for i := 0; i < max(len(want), len(got)); i++ {
		var w, g string
		if i < len(want) {
			w = want[i]
		}
		if i < len(got) {
			g = got[i]
		}
		if w != g {
			fmt.Fprintf(&b, "first difference at line %d:\nwant: %.300s\ngot:  %.300s\n", i, w, g)
			break
		}
	}
	return b.String()
}
