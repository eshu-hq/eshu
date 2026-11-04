// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9959 disposable PostgreSQL API proof that a configured ghsa target is a well-formed source and never changes an unrelated CVE read
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

// TestSupplyChainImpactReadinessGHSATargetKeepsUnrelatedCVEReadLive proves the
// readiness target resolver accepts a ghsa target the workflow validator
// admits. A ghsa target carries no CVE selector and no ecosystem, so it is not
// a candidate for a CVE read; it must not make the unrelated nvd read report
// source_targets_malformed.
func TestSupplyChainImpactReadinessGHSATargetKeepsUnrelatedCVEReadLive(t *testing.T) {
	ctx, db, now := readinessScopeLiveDatabase(t)
	seedReadinessScopeInstance(t, ctx, db, now, "nvd-instance", []map[string]any{{
		"source": "nvd", "scope_id": "healthy", "cve_ids": []string{"CVE-2026-9227"},
	}})
	seedReadinessScopeState(t, ctx, db, now, "nvd-instance", "healthy", "nvd", "fresh", "succeeded")
	before := readReadinessScopeAPI(t, db, "CVE-2026-9227")

	seedReadinessScopeInstance(t, ctx, db, now, "ghsa-instance", []map[string]any{{
		"source": "ghsa", "scope_id": "vuln-intel://ghsa/reviewed", "advisory_type": "reviewed",
		"modified_start": "2026-05-01T00:00:00Z", "modified_end": "2026-05-02T00:00:00Z",
		"results_per_page": 100, "max_requests": 10, "api_key_env": "GHSA_TOKEN",
	}})
	after := readReadinessScopeAPI(t, db, "CVE-2026-9227")
	if after.State != before.State {
		t.Fatalf("an unrelated ghsa target moved the CVE read from %q to %q", before.State, after.State)
	}
	if _, err := impact.NewPostgresReadinessStore(db).ReadSupplyChainImpactReadiness(ctx,
		impact.ReadinessQuery{CVEID: "CVE-2026-9227"}); err != nil {
		t.Fatalf("readiness read failed with a ghsa target present: %v", err)
	}
}
