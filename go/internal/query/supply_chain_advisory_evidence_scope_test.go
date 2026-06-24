// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSupplyChainListAdvisoryEvidenceResolvesRepositoryScopedFindings(t *testing.T) {
	t.Parallel()

	content := &countingRepositoryContentStore{
		fakePortContentStore: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{{
				ID:        "repo://example/api",
				Name:      "payments-api",
				LocalPath: "/srv/payments-api",
				RepoSlug:  "example/payments-api",
			}},
		},
	}
	advisoryStore := &recordingAdvisoryEvidenceStore{
		rows: []AdvisoryEvidenceRow{{
			AdvisoryKey: "CVE-2026-0001",
			CanonicalID: "CVE-2026-0001",
			CVEIDs:      []string{"CVE-2026-0001"},
			GHSAIDs:     []string{"GHSA-aaaa-bbbb-cccc"},
			AffectedPackages: []AdvisoryAffectedPackage{{
				PackageID: "pkg:npm/example",
			}},
		}},
	}
	handler := &SupplyChainHandler{Content: content, AdvisoryEvidence: advisoryStore}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/advisories/evidence?repository_id=payments-api&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got := content.matchCalls; got != 1 {
		t.Fatalf("MatchRepositories calls = %d, want 1", got)
	}
	if got, want := advisoryStore.lastFilter.RepositoryID, "repo://example/api"; got != want {
		t.Fatalf("advisory RepositoryID = %q, want %q", got, want)
	}
	if advisoryStore.lastFilter.CVEID != "" || advisoryStore.lastFilter.AdvisoryID != "" || advisoryStore.lastFilter.PackageID != "" {
		t.Fatalf("advisory source filters = %#v, want repository scope delegated to read model", advisoryStore.lastFilter)
	}
	if got, want := advisoryStore.lastFilter.Limit, 11; got != want {
		t.Fatalf("advisory Limit = %d, want %d", got, want)
	}

	var resp struct {
		Advisories []AdvisoryEvidenceRow `json:"advisories"`
		Scope      map[string]string     `json:"scope"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got := len(resp.Advisories); got != 1 {
		t.Fatalf("len(advisories) = %d, want 1", got)
	}
	if got, want := resp.Advisories[0].AdvisoryKey, "CVE-2026-0001"; got != want {
		t.Fatalf("advisory key = %q, want %q", got, want)
	}
	if got, want := resp.Scope["repository_id"], "repo://example/api"; got != want {
		t.Fatalf("scope.repository_id = %q, want %q; body = %s", got, want, w.Body.String())
	}
}

func TestSupplyChainListAdvisoryEvidenceRejectsUnknownRepositorySelectorBeforeRead(t *testing.T) {
	t.Parallel()

	content := &countingRepositoryContentStore{
		fakePortContentStore: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{{
				ID:   "repo://example/api",
				Name: "payments-api",
			}},
		},
	}
	advisoryStore := &recordingAdvisoryEvidenceStore{}
	handler := &SupplyChainHandler{Content: content, AdvisoryEvidence: advisoryStore}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/advisories/evidence?repository_id=unknown-repo&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got := advisoryStore.calls; got != 0 {
		t.Fatalf("advisory store calls = %d, want 0", got)
	}
	if body := w.Body.String(); body == "" ||
		!containsAll(body, "repository selector", "unknown-repo", "did not match") {
		t.Fatalf("body = %q, want clear selector error", body)
	}
}

func TestPageAdvisoryEvidenceRowsTrustsImpactScopeForAdvisoryAliases(t *testing.T) {
	t.Parallel()

	rows := []AdvisoryEvidenceRow{{
		AdvisoryKey: "CVE-2026-0001",
		CanonicalID: "CVE-2026-0001",
		CVEIDs:      []string{"CVE-2026-0001"},
	}}
	got := pageAdvisoryEvidenceRows(rows, AdvisoryEvidenceFilter{
		AdvisoryID:   "GHSA-aaaa-bbbb-cccc",
		RepositoryID: "repo://example/api",
		Limit:        10,
	})
	if len(got) != 1 || got[0].CanonicalID != "CVE-2026-0001" {
		t.Fatalf("repository-scoped advisory alias page = %#v, want CVE evidence row from reducer-selected scope", got)
	}
}

func TestAdvisoryEvidenceQueryDerivesRepositoryScopeFromImpactFindings(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"impact_selector AS MATERIALIZED",
		"impact_candidates AS MATERIALIZED",
		"fact.fact_kind = 'reducer_supply_chain_impact_finding'",
		"fact.payload->>'repository_id' = selector.repository_id",
		"fact.payload->'service_ids' ? selector.service_id",
		"fact.payload->'workload_ids' ? selector.workload_id",
		"SELECT payload->>'cve_id' AS value",
		"SELECT payload->>'advisory_id' AS value",
		"SELECT payload->>'package_id' AS value",
		"WHERE NULLIF(payload->>'cve_id', '') IS NULL",
		"AND NULLIF(payload->>'advisory_id', '') IS NULL",
	} {
		if !strings.Contains(listAdvisoryEvidenceQuery, want) {
			t.Fatalf("listAdvisoryEvidenceQuery missing %q:\n%s", want, listAdvisoryEvidenceQuery)
		}
	}
	if strings.Contains(listAdvisoryEvidenceQuery, "security_alert") {
		t.Fatalf("advisory evidence query must not derive advisory anchors from provider security alerts:\n%s", listAdvisoryEvidenceQuery)
	}
}
