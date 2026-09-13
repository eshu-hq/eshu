// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// This file is a thin route-wiring smoke test for the six routes below
// (#6642 Part A). scripts/verify-route-coverage.sh scopes its
// `func Test*<Method>*(` search to the handler's own directory, so it cannot
// see the full behavioral coverage these routes already have in root
// (aws_runtime_drift_test.go, replatforming_ownership_handler_test.go,
// replatforming_plan_handler_test.go, replatforming_plan_waves_handler_test.go,
// replatforming_rollups_handler_test.go, replatforming_selectors_handler_test.go,
// dead_iac_test.go and its siblings), which drive these same routes through
// the root alias (type IaCHandler = iac.Handler) and stayed in root because
// they need root-only fixtures or genuine production auth middleware this
// leaf cannot import. Each smoke test below only proves the route is
// mounted and dispatches into the named handler method (a non-404 response);
// it makes no behavioral assertion, because that proof already exists.

// smokeReachabilityStore is a minimal ReachabilityStore returning empty
// results, sufficient to let handleDeadIaC's route dispatch complete.
type smokeReachabilityStore struct{}

func (smokeReachabilityStore) ListLatestCleanupFindings(
	context.Context, []string, []string, bool, int, int,
) ([]ReachabilityFindingRow, error) {
	return nil, nil
}

func (smokeReachabilityStore) CountLatestCleanupFindings(
	context.Context, []string, []string, bool,
) (int, error) {
	return 0, nil
}

func (smokeReachabilityStore) HasLatestRows(context.Context, []string, []string) (bool, error) {
	return false, nil
}

// smokeManagementAndSelectorStore implements both ManagementStore and
// ReplatformingSelectorStore -- handleReplatformingSelectors type-asserts
// h.Management to ReplatformingSelectorStore (replatforming_selectors_handler.go),
// so one fake must satisfy both interfaces for that route's dispatch to
// reach its handler body instead of the graceful ok-false branch.
type smokeManagementAndSelectorStore struct {
	fakeIaCManagementStore
}

func (smokeManagementAndSelectorStore) ListReplatformingSelectors(
	context.Context, int, []string,
) (ReplatformingSelectorPage, error) {
	return ReplatformingSelectorPage{}, nil
}

func smokeHandler() *Handler {
	return &Handler{
		Profile:      querycontract.ProfileLocalAuthoritative,
		Reachability: smokeReachabilityStore{},
		Management:   fakeIaCManagementStore{},
	}
}

func TestHandleDeadIaCSmoke(t *testing.T) {
	t.Parallel()

	handler := smokeHandler()
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/iac/dead", bytes.NewBufferString(`{}`))
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code == http.StatusNotFound {
		t.Fatalf("POST /api/v0/iac/dead is not mounted: status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestHandleAWSRuntimeDriftFindingsSmoke(t *testing.T) {
	t.Parallel()

	handler := smokeHandler()
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/aws/runtime-drift/findings", bytes.NewBufferString(`{}`))
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code == http.StatusNotFound {
		t.Fatalf("POST /api/v0/aws/runtime-drift/findings is not mounted: status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestHandleReplatformingSelectorsSmoke(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		Profile:      querycontract.ProfileLocalAuthoritative,
		Reachability: smokeReachabilityStore{},
		Management:   smokeManagementAndSelectorStore{},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/replatforming/selectors", nil)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code == http.StatusNotFound {
		t.Fatalf("GET /api/v0/replatforming/selectors is not mounted: status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestHandleReplatformingRollupsSmoke(t *testing.T) {
	t.Parallel()

	handler := smokeHandler()
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/replatforming/rollups", bytes.NewBufferString(`{}`))
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code == http.StatusNotFound {
		t.Fatalf("POST /api/v0/replatforming/rollups is not mounted: status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestHandleReplatformingPlanSmoke(t *testing.T) {
	t.Parallel()

	handler := smokeHandler()
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, ReplatformingPlanRoute, bytes.NewBufferString(`{}`))
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code == http.StatusNotFound {
		t.Fatalf("POST %s is not mounted: status = %d, body = %s", ReplatformingPlanRoute, w.Code, w.Body.String())
	}
}

func TestHandleReplatformingOwnershipPacketsSmoke(t *testing.T) {
	t.Parallel()

	handler := smokeHandler()
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/replatforming/ownership-packets", bytes.NewBufferString(`{}`))
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code == http.StatusNotFound {
		t.Fatalf("POST /api/v0/replatforming/ownership-packets is not mounted: status = %d, body = %s", w.Code, w.Body.String())
	}
}
