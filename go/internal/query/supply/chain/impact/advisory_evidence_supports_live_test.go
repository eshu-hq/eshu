// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10880 PR-B2a disposable PostgreSQL 18 proof of the supports-sourced advisory evidence on the multi-support corpus: for every repository anchor and grant shape, the evidence derived from the per-support table equals the legacy fact read, including a repository whose supports sit in two scopes and an operator-scope row; a mutation that ignores the active generation or the repository anchor fails; a supports table that is not ready refuses; reads ESHU_GROUPED_INVENTORY_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_GROUPED_INVENTORY_PROOF_DISPOSABLE ESHU_GROUPED_INVENTORY_PROOF_DSN

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/advisory"
)

// TestAdvisoryEvidenceFromSupportsLive holds the supports-sourced advisory
// evidence to the legacy fact read: the supports table mirrors every live
// finding fact, so the advisory anchors a repository selector derives, and the
// evidence they select, are the same rows.
func TestAdvisoryEvidenceFromSupportsLive(t *testing.T) {
	ctx, database := openSupportsEqualityDatabase(t)
	findings, err := PostgresFindingStore{DB: database}.ListSupplyChainImpactFindings(ctx, FindingFilter{Status: "affected_exact", Limit: 200})
	if err != nil || len(findings) == 0 {
		t.Fatalf("seed list = %d rows, %v", len(findings), err)
	}
	groupedLiveScope(t, ctx, database, "vuln:feed", "gen:vuln")
	cves, repositories := map[string]bool{}, map[string]bool{}
	for _, finding := range findings {
		cves[finding.CVEID], repositories[finding.RepositoryID] = true, true
	}
	n := 0
	for cve := range cves {
		n++
		groupedLiveFact(t, ctx, database, "vuln:"+cve, "vuln:feed", "gen:vuln", "vulnerability.cve",
			map[string]any{"cve_id": cve, "source": "osv", "advisory_id": "GHSA-" + cve, "summary": "seeded"})
	}

	legacy := advisory.PostgresEvidenceStore{DB: database}
	supports := advisory.PostgresEvidenceStore{DB: database, ReadFromWinners: true}
	grants := [][]string{nil, {"repository:m1", "repository:m2"}, {multiSupportScopeA}, {"repository:none"}}
	compared, nonEmpty := 0, 0
	for repository := range repositories {
		for _, grant := range grants {
			t.Run(repository, func(t *testing.T) {
				filter := advisory.EvidenceFilter{RepositoryID: repository, Limit: 50, AllowedSourceRepositoryIDs: grant}
				want, err := legacy.ListAdvisoryEvidence(ctx, filter)
				if err != nil {
					t.Fatalf("legacy evidence: %v", err)
				}
				got, err := supports.ListAdvisoryEvidence(ctx, filter)
				if err != nil {
					t.Fatalf("supports evidence: %v", err)
				}
				wantJSON, _ := json.Marshal(want)
				gotJSON, _ := json.Marshal(got)
				if string(wantJSON) != string(gotJSON) {
					t.Fatalf("supports evidence differs from legacy for %s with grant %v\nlegacy:   %s\nsupports: %s", repository, grant, wantJSON, gotJSON)
				}
				compared++
				if len(got) > 0 {
					nonEmpty++
				}
			})
		}
	}
	if compared == 0 || nonEmpty == 0 {
		t.Fatalf("compared %d cases and %d non-empty (of %d seeded advisories): the matrix lost its signal", compared, nonEmpty, n)
	}

	t.Run("a supports table that is not ready refuses", func(t *testing.T) {
		if _, err := database.ExecContext(ctx, `INSERT INTO supply_chain_impact_finding_supports_dirty (scope_id) VALUES ('scope:dirty')`); err != nil {
			t.Fatalf("fence: %v", err)
		}
		_, scoped := supports.ListAdvisoryEvidence(ctx, advisory.EvidenceFilter{RepositoryID: "repository:m1", Limit: 5})
		_, cveOnly := supports.ListAdvisoryEvidence(ctx, advisory.EvidenceFilter{CVEID: "CVE-2026-m1", Limit: 5})
		if _, err := database.ExecContext(ctx, `DELETE FROM supply_chain_impact_finding_supports_dirty`); err != nil {
			t.Fatalf("restore: %v", err)
		}
		if !errors.Is(scoped, advisory.ErrEvidenceSupportsUnavailable) {
			t.Fatalf("scoped evidence while not ready = %v, want ErrEvidenceSupportsUnavailable", scoped)
		}
		if cveOnly != nil {
			t.Fatalf("a bare cve_id call reads no impact finding and must not refuse: %v", cveOnly)
		}
	})
}
