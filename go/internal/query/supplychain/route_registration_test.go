// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychain

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// serveSupplyChainRoute mounts a bare production-profile handler and serves
// one request through the real mux registrations. The bare handler carries
// no stores, so every route answers with its fail-closed contract envelope
// (501/503) or an empty-scope short-circuit -- never by touching a store.
// A 404 means the route is not registered, which is exactly what these
// tests pin: after the #6060 hub move, verify-route-coverage requires a
// same-package test for every moved HandleFunc registration, and deep
// behavior stays pinned by the root behavior suites.
func serveSupplyChainRoute(t *testing.T, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()

	handler := &SupplyChainHandler{Profile: querycontract.ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func assertRouteRegistered(t *testing.T, method, target, body string) {
	t.Helper()

	rec := serveSupplyChainRoute(t, method, target, body)
	if rec.Code == http.StatusNotFound {
		t.Fatalf("%s %s: route not registered on supply-chain mux (got 404)", method, target)
	}
}

func TestCountContainerImageIdentitiesRoute(t *testing.T) {
	t.Parallel()
	assertRouteRegistered(t, http.MethodGet,
		"/api/v0/supply-chain/container-images/identities/count", "")
}

func TestContainerImageIdentityInventoryRoute(t *testing.T) {
	t.Parallel()
	assertRouteRegistered(t, http.MethodGet,
		"/api/v0/supply-chain/container-images/identities/inventory", "")
}

func TestCountSBOMAttestationAttachmentsRoute(t *testing.T) {
	t.Parallel()
	assertRouteRegistered(t, http.MethodGet,
		"/api/v0/supply-chain/sbom-attestations/attachments/count", "")
}

func TestSBOMAttestationAttachmentInventoryRoute(t *testing.T) {
	t.Parallel()
	assertRouteRegistered(t, http.MethodGet,
		"/api/v0/supply-chain/sbom-attestations/attachments/inventory", "")
}

func TestCountSecurityAlertReconciliationsRoute(t *testing.T) {
	t.Parallel()
	assertRouteRegistered(t, http.MethodGet,
		"/api/v0/supply-chain/security-alerts/reconciliations/count", "")
}

func TestSecurityAlertReconciliationInventoryRoute(t *testing.T) {
	t.Parallel()
	assertRouteRegistered(t, http.MethodGet,
		"/api/v0/supply-chain/security-alerts/reconciliations/inventory", "")
}

func TestCountImpactFindingsRoute(t *testing.T) {
	t.Parallel()
	assertRouteRegistered(t, http.MethodGet,
		"/api/v0/supply-chain/impact/findings/count", "")
}

func TestImpactInventoryRoute(t *testing.T) {
	t.Parallel()
	assertRouteRegistered(t, http.MethodGet,
		"/api/v0/supply-chain/impact/inventory", "")
}

func TestGetVulnerabilityScannerReadContractRoute(t *testing.T) {
	t.Parallel()
	assertRouteRegistered(t, http.MethodGet,
		"/api/v0/supply-chain/vulnerability-scanner/contract", "")
}

func TestListSBOMAttachmentsRoute(t *testing.T) {
	t.Parallel()
	assertRouteRegistered(t, http.MethodGet,
		"/api/v0/supply-chain/sbom-attestations/attachments", "")
}

func TestListAdvisoryCatalogRoute(t *testing.T) {
	t.Parallel()
	assertRouteRegistered(t, http.MethodGet,
		"/api/v0/supply-chain/advisories", "")
}

func TestListAdvisoryEvidenceRoute(t *testing.T) {
	t.Parallel()
	assertRouteRegistered(t, http.MethodGet,
		"/api/v0/supply-chain/advisories/evidence", "")
}

func TestGetVulnerabilityDetailRoute(t *testing.T) {
	t.Parallel()
	assertRouteRegistered(t, http.MethodGet,
		"/api/v0/supply-chain/vulnerabilities/ADV-1", "")
}

func TestExplainImpactRoute(t *testing.T) {
	t.Parallel()
	assertRouteRegistered(t, http.MethodGet,
		"/api/v0/supply-chain/impact/explain?finding_id=finding-1", "")
}

func TestCreateVulnerabilitySuppressionRoute(t *testing.T) {
	t.Parallel()
	assertRouteRegistered(t, http.MethodPost,
		"/api/v0/supply-chain/impact/suppressions", `{"advisory_id":"ADV-1"}`)
}

func TestListContainerImageIdentitiesRoute(t *testing.T) {
	t.Parallel()
	assertRouteRegistered(t, http.MethodGet,
		"/api/v0/supply-chain/container-images/identities", "")
}

func TestListSecurityAlertReconciliationsRoute(t *testing.T) {
	t.Parallel()
	assertRouteRegistered(t, http.MethodGet,
		"/api/v0/supply-chain/security-alerts/reconciliations", "")
}
