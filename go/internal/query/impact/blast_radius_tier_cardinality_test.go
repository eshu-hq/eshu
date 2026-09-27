// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// TestEnrichBlastRadiusTiersWithholdsAmbiguousTier is the #6590 regression
// for enrichBlastRadiusTiers's unenforced "one tier per repo" assumption.
// blastRadiusTierLookupCypher carries no LIMIT or DISTINCT, and no :Tier
// writer exists in-tree to guarantee single membership -- so a repo that
// resolves to more than one distinct (tier, risk) pair must have its
// tier/risk withheld (no keys set) rather than silently picking whichever
// row happened to be read last out of a Go map. A repo with only duplicate
// identical rows is not ambiguous and must still get its tier/risk merged.
func TestEnrichBlastRadiusTiersWithholdsAmbiguousTier(t *testing.T) {
	t.Parallel()

	var logBuf strings.Builder
	handler := &Handler{
		Logger: slog.New(slog.NewJSONHandler(&logBuf, nil)),
		Neo4j: graph.FakeGraphReader{
			RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "CONTAINS]-(tier:Tier)") {
					t.Fatalf("unexpected cypher: %s", cypher)
				}
				return []map[string]any{
					{"repo_id": "repo-web", "tier": "tier-1", "risk": "critical"},
					{"repo_id": "repo-web", "tier": "tier-2", "risk": "low"},
					{"repo_id": "repo-api", "tier": "tier-1", "risk": "critical"},
					{"repo_id": "repo-api", "tier": "tier-1", "risk": "critical"},
				}, nil
			},
		},
	}

	affected := []map[string]any{
		{"repo_id": "repo-web"},
		{"repo_id": "repo-api"},
	}
	handler.enrichBlastRadiusTiers(context.Background(), affected)

	if _, ok := affected[0]["tier"]; ok {
		t.Fatalf("ambiguous repo-web row must withhold tier, got %#v", affected[0])
	}
	if _, ok := affected[0]["risk"]; ok {
		t.Fatalf("ambiguous repo-web row must withhold risk, got %#v", affected[0])
	}
	if affected[1]["tier"] != "tier-1" || affected[1]["risk"] != "critical" {
		t.Fatalf("repo-api (duplicate identical rows, not ambiguous) must keep its tier/risk, got %#v", affected[1])
	}

	logged := logBuf.String()
	if !strings.Contains(logged, "blast-radius tier enrichment ambiguous") {
		t.Fatalf("expected an ambiguous-tier warning log, got %q", logged)
	}
	if !strings.Contains(logged, `"repo_id":"repo-web"`) {
		t.Fatalf("expected the ambiguous warning to name repo_id=repo-web, got %q", logged)
	}
	if strings.Contains(logged, "repo-api") {
		t.Fatalf("repo-api is not ambiguous and must not be logged as such, got %q", logged)
	}
}
