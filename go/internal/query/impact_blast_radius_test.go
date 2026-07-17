// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBlastRadiusQueriesAreNornicDBSafe guards the #5279 fix: every blast-radius
// affected query must avoid the multi-clause shapes the pinned NornicDB build
// silently mis-executes (RETURN DISTINCT / length(path) under OPTIONAL MATCH,
// untyped var-length traversal, zero-length *0.., a trailing clause after
// CALL{}) and must use the proven-safe shapes instead. These assertions fail on
// the pre-#5279 queries, which returned literal alias text ("DISTINCT
// affected.name") and affected_count:1 for a 19-repo blast radius.
func TestBlastRadiusQueriesAreNornicDBSafe(t *testing.T) {
	t.Parallel()

	affected := map[string]string{
		"repository":       blastRadiusRepositoryCypher,
		"terraform_source": blastRadiusTerraformSourceReposCypher,
		"terraform_deps":   blastRadiusDependentsByNameCypher,
		"crossplane":       blastRadiusCrossplaneCypher,
		"sql_table":        blastRadiusSqlTableCypher,
	}
	for name, q := range affected {
		if strings.Contains(q, "OPTIONAL MATCH") {
			t.Errorf("%s query must not use OPTIONAL MATCH (multi-clause literal-text / row-drop on NornicDB): %s", name, q)
		}
		if strings.Contains(q, "RETURN DISTINCT") {
			t.Errorf("%s query must not use RETURN DISTINCT (multi-clause returns literal alias text on NornicDB): %s", name, q)
		}
		if strings.Contains(q, "all(rel IN rels") {
			t.Errorf("%s query must not use untyped rels + all(type(rel)=...) (matches zero rows on NornicDB): %s", name, q)
		}
		if strings.Contains(q, "*0..") {
			t.Errorf("%s query must not use a zero-length var-length path (*0.. projects literal text on NornicDB): %s", name, q)
		}
	}

	// The dependent traversals must be TYPED DEPENDS_ON var-length with an
	// in-clause min(length(path)) hop metric.
	for _, name := range []string{"repository", "terraform_deps"} {
		q := affected[name]
		if !strings.Contains(q, ":DEPENDS_ON*1..5") {
			t.Errorf("%s query must use typed :DEPENDS_ON*1..5 traversal: %s", name, q)
		}
		if !strings.Contains(q, "min(length(path)) AS hops") {
			t.Errorf("%s query must compute hops via min(length(path)) in the single clause: %s", name, q)
		}
	}

	// sql_table must keep the CALL{UNION} core but have NOTHING except the plain
	// outer RETURN/ORDER BY/LIMIT after the closing brace (a trailing OPTIONAL
	// MATCH there hard-errors with "unsupported clause after CALL {}").
	closeBrace := strings.LastIndex(blastRadiusSqlTableCypher, "}")
	if closeBrace < 0 {
		t.Fatal("sql_table query lost its CALL {} block")
	}
	tail := blastRadiusSqlTableCypher[closeBrace+1:]
	if strings.Contains(tail, "MATCH") || strings.Contains(tail, "WITH") {
		t.Errorf("sql_table query must have only RETURN/ORDER BY/LIMIT after CALL {}, got tail: %s", tail)
	}

	// Tier enrichment must be a separate single-clause lookup, not folded in.
	if strings.Count(blastRadiusTierLookupCypher, "MATCH") != 1 || strings.Contains(blastRadiusTierLookupCypher, "OPTIONAL") {
		t.Errorf("tier lookup must be a single-clause MATCH query: %s", blastRadiusTierLookupCypher)
	}
}

// TestFindBlastRadiusRepositoryMergesAffectedAndTiers proves the handler runs
// the affected query and the separate tier query and merges them into correct,
// non-garbage output — the exact scenario that produced literal alias strings
// and affected_count:1 before #5279.
func TestFindBlastRadiusRepositoryMergesAffectedAndTiers(t *testing.T) {
	t.Parallel()

	var affectedCalls, tierCalls int
	handler := &ImpactHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, ":DEPENDS_ON*1..5"):
					affectedCalls++
					if got := params["target_name"]; got != "payments-core" {
						t.Fatalf("affected params[target_name] = %#v, want payments-core", got)
					}
					return []map[string]any{
						{"repo": "web", "repo_id": "repo-web", "hops": int64(1)},
						{"repo": "api", "repo_id": "repo-api", "hops": int64(1)},
						{"repo": "worker", "repo_id": "repo-worker", "hops": int64(2)},
					}, nil
				case strings.Contains(cypher, "CONTAINS]-(tier:Tier)"):
					tierCalls++
					names, _ := params["repo_names"].([]string)
					if len(names) != 3 {
						t.Fatalf("tier lookup got %d repo names, want 3", len(names))
					}
					return []map[string]any{
						{"repo": "web", "tier": "tier-1", "risk": "critical"},
					}, nil
				default:
					t.Fatalf("unexpected cypher: %s", cypher)
					return nil, nil
				}
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/blast-radius",
		bytes.NewBufferString(`{"target":"payments-core","target_type":"repository"}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	if affectedCalls != 1 || tierCalls != 1 {
		t.Fatalf("expected 1 affected + 1 tier query, got %d + %d", affectedCalls, tierCalls)
	}

	var resp struct {
		AffectedCount int `json:"affected_count"`
		Affected      []struct {
			Repo   string `json:"repo"`
			RepoID string `json:"repo_id"`
			Hops   int    `json:"hops"`
			Tier   string `json:"tier"`
			Risk   string `json:"risk"`
		} `json:"affected"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (body %s)", err, w.Body.String())
	}
	if resp.AffectedCount != 3 {
		t.Fatalf("affected_count = %d, want 3", resp.AffectedCount)
	}
	// Real repo names + hops, never literal alias text.
	byRepo := map[string]int{}
	for _, a := range resp.Affected {
		if a.Repo == "" || strings.Contains(a.Repo, "DISTINCT") || strings.Contains(a.Repo, "affected.name") {
			t.Fatalf("affected repo is literal/empty garbage: %#v", a)
		}
		byRepo[a.Repo] = a.Hops
	}
	if byRepo["web"] != 1 || byRepo["api"] != 1 || byRepo["worker"] != 2 {
		t.Fatalf("hops wrong: %#v", byRepo)
	}
	// Tier enrichment merged onto the matching repo only.
	for _, a := range resp.Affected {
		if a.Repo == "web" && (a.Tier != "tier-1" || a.Risk != "critical") {
			t.Fatalf("web tier/risk not merged: %#v", a)
		}
		if a.Repo != "web" && (a.Tier != "" || a.Risk != "") {
			t.Fatalf("non-web repo got a tier it shouldn't: %#v", a)
		}
	}
}

// TestFindBlastRadiusSqlTableTierErrorDegradesGracefully proves a tier lookup
// error does not fail the whole read — the affected set is already correct — and
// that the sql_table branch merges per-repo min hops across the UNION.
func TestFindBlastRadiusSqlTableTierErrorDegradesGracefully(t *testing.T) {
	t.Parallel()

	handler := &ImpactHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "CALL {"):
					// Same repo via two UNION branches at hops 0 and 1.
					return []map[string]any{
						{"repo": "orders-db", "repo_id": "repo-orders-db", "hops": int64(1)},
						{"repo": "orders-db", "repo_id": "repo-orders-db", "hops": int64(0)},
						{"repo": "reporting", "repo_id": "repo-reporting", "hops": int64(1)},
					}, nil
				case strings.Contains(cypher, "CONTAINS]-(tier:Tier)"):
					return nil, context.DeadlineExceeded
				default:
					t.Fatalf("unexpected cypher: %s", cypher)
					return nil, nil
				}
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/blast-radius",
		bytes.NewBufferString(`{"target":"orders","target_type":"sql_table"}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("tier error should degrade, not fail: status %d body %s", w.Code, w.Body.String())
	}
	var resp struct {
		AffectedCount int `json:"affected_count"`
		Affected      []struct {
			Repo string `json:"repo"`
			Hops int    `json:"hops"`
		} `json:"affected"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.AffectedCount != 2 {
		t.Fatalf("affected_count = %d, want 2 (orders-db deduped)", resp.AffectedCount)
	}
	for _, a := range resp.Affected {
		if a.Repo == "orders-db" && a.Hops != 0 {
			t.Fatalf("orders-db should keep min hop 0 across UNION, got %d", a.Hops)
		}
	}
}

// TestMergeBlastRadiusRowsMinHopsDedup covers the app-side min-hop dedup that
// replaced the query's DISTINCT + ORDER BY: same repo at multiple hops collapses
// to one row at the smallest hop, repo_id/claim survive, and ties sort by name.
func TestMergeBlastRadiusRowsMinHopsDedup(t *testing.T) {
	t.Parallel()

	in := []map[string]any{
		{"repo": "b", "repo_id": "repo-b", "hops": int64(3)},
		{"repo": "a", "repo_id": "repo-a", "hops": int64(2)},
		{"repo": "b", "hops": int64(1)},                 // same repo, smaller hop, missing repo_id
		{"repo": "a", "hops": int64(2), "claim": "c-a"}, // claim arrives on a later row
		{"repo": "", "hops": int64(9)},                  // dropped: empty repo
	}
	got := mergeBlastRadiusRows(in)
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2: %#v", len(got), got)
	}
	// Sorted by (hops, repo): b@1 before a@2.
	if StringVal(got[0], "repo") != "b" || IntVal(got[0], "hops") != 1 {
		t.Fatalf("row0 = %#v, want b@1", got[0])
	}
	if StringVal(got[0], "repo_id") != "repo-b" {
		t.Fatalf("b lost its repo_id from the first-seen row: %#v", got[0])
	}
	if StringVal(got[1], "repo") != "a" || IntVal(got[1], "hops") != 2 {
		t.Fatalf("row1 = %#v, want a@2", got[1])
	}
	if StringVal(got[1], "claim") != "c-a" {
		t.Fatalf("a lost the claim that arrived on a later row: %#v", got[1])
	}
}
