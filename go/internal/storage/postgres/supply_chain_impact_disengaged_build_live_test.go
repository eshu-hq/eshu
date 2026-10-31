// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9299 disposable PostgreSQL proof that the disengaged impact load runs the full legacy expansion, so the os-package advisory evidence reaches the build and a finding is derived exactly as the unwired legacy path derives it
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_POSTGRES_DISPOSABLE

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestAuthorizedImpactLoaderDisengagedBuildsAdvisoryFindingLive is the
// corpus-0 regression: with no role policy installed, the disengaged load
// must run the full legacy expansion (not the static-only snapshot), so
// the os-package advisory evidence reaches the build and a finding is
// derived exactly as the unwired legacy path derives it. A static-only
// disengaged load starves the expansion stage: zero advisory facts, zero
// findings, and the write drains empty while the corpus baseline expects
// the CVE finding.
func TestAuthorizedImpactLoaderDisengagedBuildsAdvisoryFindingLive(t *testing.T) {
	adminDSN := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	optIn := os.Getenv("ESHU_POSTGRES_DISPOSABLE")
	if adminDSN == "" {
		t.Skip("set ESHU_POSTGRES_DSN and ESHU_POSTGRES_DISPOSABLE=1 to run the disengaged-build proof")
	}
	ctx, database := postgresproof.OpenDisposableBootstrapDatabase(t, adminDSN, optIn, 5*time.Minute,
		func(ctx context.Context, database *sql.DB) error {
			return postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: database})
		})
	replaceSetSeedScope(t, ctx, database)
	cappedSeedIntelFact(t, ctx, database, "vuln-cve:disengaged", "vulnerability.cve",
		`{"cve_id":"CVE-2026-9299","advisory_id":"CVE-2026-9299","cvss_score":8.1,"aliases":["CVE-2026-9299"]}`)
	cappedSeedIntelFact(t, ctx, database, "vuln-affected:disengaged", "vulnerability.affected_package",
		`{"cve_id":"CVE-2026-9299","advisory_id":"CVE-2026-9299","package_id":"pkg:npm/disengaged",
		  "ecosystem":"npm","package_name":"disengaged","affected_versions":["1.2.3"],"fixed_versions":["1.3.0"]}`)
	cappedSeedIntelFact(t, ctx, database, "package-consumption:disengaged", "reducer_package_consumption_correlation",
		`{"package_id":"pkg:npm/disengaged","relationship_kind":"consumption","repository_id":"repo://example/disengaged",
		  "dependency_range":"1.2.3","canonical_writes":1,"evidence_fact_ids":["manifest-lock-1"]}`)
	capture := &capturingImpactWriter{inner: replaceSetWriter(database)}
	handler := supplychaincore.SupplyChainImpactHandler{
		FencingTokenIssuer:              postgres.PostgresSupplyChainImpactFencingTokenIssuer{DB: postgres.SQLDB{DB: database}},
		FactLoader:                      postgres.NewFactStore(postgres.SQLDB{DB: database}),
		AuthorizedEvidenceLoader:        postgres.NewAuthorizedSupplyChainImpactEvidenceLoader(postgres.SQLDB{DB: database}),
		RequireAuthorizedEvidenceLoader: true,
		Writer:                          capture,
	}
	result, err := handler.Handle(ctx, reducercontract.Intent{
		IntentID:     "intent:disengaged-build",
		ScopeID:      replaceSetLiveScope,
		GenerationID: replaceSetLiveGeneration,
		SourceSystem: "vulnerability_intelligence",
		Domain:       reducercontract.DomainSupplyChainImpact,
		Cause:        "synthetic #9299 disengaged-build proof",
	})
	if err != nil {
		t.Fatalf("Handle() without installed policy: %v", err)
	}
	t.Logf("disengaged subsignals: os_package_advisory_facts=%v scope_facts=%v active_evidence_facts=%v input_invalid_facts=%v evidence_findings=%v",
		result.SubSignals["os_package_advisory_facts"], result.SubSignals["scope_facts"],
		result.SubSignals["active_evidence_facts"], result.SubSignals["input_invalid_facts"],
		result.SubSignals["evidence_findings"])
	if len(capture.write.Findings) == 0 {
		t.Fatal("disengaged handler built 0 findings from advisory evidence: the plain path must run the full legacy expansion, not the static-only snapshot")
	}
}
