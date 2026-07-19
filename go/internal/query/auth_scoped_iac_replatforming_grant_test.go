// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// #5167 W4 two-tenant grant proof for the iac/replatforming AWS-scope-filtered
// route family: find_unmanaged_resources, get_iac_management_status,
// explain_iac_management_status, propose_terraform_import_plan,
// compose_replatforming_plan, get_replatforming_rollups, and
// find_unmanaged_resource_owners. All seven share one choke point --
// normalizeIaCManagementRequest -> bindIaCManagementFilterAccess ->
// IaCManagementStore.{List,Count}UnmanagedCloudResources (iac_management.go)
// -- so a single fakeIaCManagementStore (iac_management_test.go) whose
// List/Count faithfully mirror postgres.AWSCloudRuntimeDriftFindingStore's
// grant-intersection contract (aws_cloud_runtime_drift_findings.go) proves
// every route's binding.
//
// Both assertions below are mutation-sensitive by construction:
//   - Removing a handler's bindIaCManagementFilterAccess call leaves
//     filter.Scoped at its zero value (false), which fakeIaCManagementStore
//     treats as "unrestricted" (matching pre-#5167-W4 production behavior),
//     so the cross-tenant finding leaks into the response and the "response
//     never contains the other-tenant ARN" assertion fails.
//   - Removing bindIaCManagementFilterAccess also means an empty-grant caller
//     never sets filter.Scoped, so fakeIaCManagementStore's zero-rows-without-
//     touching-the-store guard never engages and dbTouched flips true,
//     failing the short-circuit assertion.
//
// A same-AWS-account, different-region/team scope pair is used for the two
// synthetic findings (rather than two different AWS accounts) because that is
// the harder case: an account_id-only filter cannot distinguish the two rows
// on its own, so the ONLY thing preventing the cross-tenant leak is the exact
// scope-id grant intersection under test.
const (
	iacGrantTestAccountID    = "111111111111"
	iacGrantTestGrantedScope = "aws:111111111111:us-east-1:lambda"
	iacGrantTestOtherScope   = "aws:111111111111:us-west-2:lambda"
	iacGrantTestGrantedARN   = "arn:aws:lambda:us-east-1:111111111111:function:tenant-a-fn"
	iacGrantTestOtherARN     = "arn:aws:lambda:us-west-2:111111111111:function:tenant-b-fn"
)

func iacGrantTestFindings() []IaCManagementFindingRow {
	return []IaCManagementFindingRow{
		{
			ID:               iacGrantTestGrantedARN,
			ARN:              iacGrantTestGrantedARN,
			ScopeID:          iacGrantTestGrantedScope,
			AccountID:        iacGrantTestAccountID,
			Region:           "us-east-1",
			FindingKind:      findingKindUnmanagedCloudResource,
			ManagementStatus: managementStatusCloudOnly,
			ResourceType:     "lambda",
			ResourceID:       "tenant-a-fn",
		},
		{
			ID:               iacGrantTestOtherARN,
			ARN:              iacGrantTestOtherARN,
			ScopeID:          iacGrantTestOtherScope,
			AccountID:        iacGrantTestAccountID,
			Region:           "us-west-2",
			FindingKind:      findingKindUnmanagedCloudResource,
			ManagementStatus: managementStatusCloudOnly,
			ResourceType:     "lambda",
			ResourceID:       "tenant-b-fn",
		},
	}
}

func iacGrantScopedAuthContext(allowedScopeIDs []string) AuthContext {
	return AuthContext{
		Mode:            AuthModeScoped,
		TenantID:        "tenant-a",
		WorkspaceID:     "workspace-a",
		AllowedScopeIDs: allowedScopeIDs,
	}
}

type iacManagementFamilyRoute struct {
	name string
	path string
	body map[string]any
	// includesRawARN is true when the route's response body embeds each
	// finding's raw arn field, so the test can assert directly on the
	// tenant-a/tenant-b ARN strings. The rollup route only ever emits
	// aggregate counts (dimensions, source_state_totals) -- never a raw ARN --
	// by design, so its row-data proof instead checks total_findings_count.
	includesRawARN bool
}

// iacManagementFamilyRoutes lists the five bulk-page routes on the shared
// IaCManagementFilter choke point. get_iac_management_status and
// explain_iac_management_status are exact single-finding lookups with a
// different request shape and are covered separately below
// (TestIaCManagementStatusRoutesEnforceScopeGrant).
func iacManagementFamilyRoutes() []iacManagementFamilyRoute {
	return []iacManagementFamilyRoute{
		{
			name:           "find_unmanaged_resources",
			path:           "/api/v0/iac/unmanaged-resources",
			body:           map[string]any{"account_id": iacGrantTestAccountID},
			includesRawARN: true,
		},
		{
			name:           "propose_terraform_import_plan",
			path:           "/api/v0/iac/terraform-import-plan/candidates",
			body:           map[string]any{"account_id": iacGrantTestAccountID},
			includesRawARN: true,
		},
		{
			name:           "compose_replatforming_plan",
			path:           replatformingPlanRoute,
			body:           map[string]any{"account_id": iacGrantTestAccountID, "scope_kind": "account"},
			includesRawARN: true,
		},
		{
			name: "get_replatforming_rollups",
			path: "/api/v0/replatforming/rollups",
			body: map[string]any{"account_id": iacGrantTestAccountID},
		},
		{
			name:           "find_unmanaged_resource_owners",
			path:           "/api/v0/replatforming/ownership-packets",
			body:           map[string]any{"account_id": iacGrantTestAccountID},
			includesRawARN: true,
		},
	}
}

// iacManagementFamilyTotalFindingsCount extracts the response envelope's
// data.total_findings_count field, the one row-data signal every route in
// iacManagementFamilyRoutes() reports regardless of whether it also embeds
// raw ARNs.
func iacManagementFamilyTotalFindingsCount(t *testing.T, body []byte) int {
	t.Helper()
	var envelope ResponseEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope.Data = %#v, want map[string]any", envelope.Data)
	}
	count, ok := data["total_findings_count"].(float64)
	if !ok {
		t.Fatalf("data[total_findings_count] = %#v, want a number", data["total_findings_count"])
	}
	return int(count)
}

func newIaCManagementRouteRequest(t *testing.T, path string, body map[string]any, auth *AuthContext) *http.Request {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal(body) error = %v, want nil", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
	req.Header.Set("Accept", EnvelopeMIMEType)
	if auth != nil {
		req = req.WithContext(ContextWithAuthContext(req.Context(), *auth))
	}
	return req
}

// TestIaCManagementFamilyRoutesFilterByScopeGrant proves axis (b): a caller
// granted only the tenant-a AWS scope never sees the tenant-b finding, even
// though both findings satisfy the request's account_id filter and both live
// in the same fakeIaCManagementStore.
func TestIaCManagementFamilyRoutesFilterByScopeGrant(t *testing.T) {
	t.Parallel()

	for _, route := range iacManagementFamilyRoutes() {
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()

			store := fakeIaCManagementStore{rows: iacGrantTestFindings()}
			handler := &IaCHandler{Profile: ProfileLocalAuthoritative, Management: store}
			mux := http.NewServeMux()
			handler.Mount(mux)

			auth := iacGrantScopedAuthContext([]string{iacGrantTestGrantedScope})
			req := newIaCManagementRouteRequest(t, route.path, route.body, &auth)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			if got, want := iacManagementFamilyTotalFindingsCount(t, rec.Body.Bytes()), 1; got != want {
				t.Fatalf("total_findings_count = %d, want %d (only the granted-scope finding): %s", got, want, rec.Body.String())
			}
			body := rec.Body.String()
			if route.includesRawARN && !strings.Contains(body, iacGrantTestGrantedARN) {
				t.Fatalf("response missing granted-scope ARN %q: %s", iacGrantTestGrantedARN, body)
			}
			if strings.Contains(body, iacGrantTestOtherARN) {
				t.Fatalf("response leaked out-of-grant ARN %q: %s", iacGrantTestOtherARN, body)
			}
		})
	}
}

// TestIaCManagementFamilyRoutesEmptyGrantShortCircuits proves axis (a): a
// scoped caller with no granted AWS collector scope gets a bounded empty
// result AND fakeIaCManagementStore never touches its backing rows (the same
// no-query-on-empty-grant guarantee
// postgres.AWSCloudRuntimeDriftFindingStore.{List,Count}ActiveFindings
// implement) -- not "query then filter to empty."
func TestIaCManagementFamilyRoutesEmptyGrantShortCircuits(t *testing.T) {
	t.Parallel()

	for _, route := range iacManagementFamilyRoutes() {
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()

			var dbTouched bool
			store := fakeIaCManagementStore{rows: iacGrantTestFindings(), dbTouched: &dbTouched}
			handler := &IaCHandler{Profile: ProfileLocalAuthoritative, Management: store}
			mux := http.NewServeMux()
			handler.Mount(mux)

			auth := iacGrantScopedAuthContext(nil)
			req := newIaCManagementRouteRequest(t, route.path, route.body, &auth)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			if dbTouched {
				t.Fatal("dbTouched = true, want false -- an empty scoped grant must skip the store read entirely, not query then filter to empty")
			}
			body := rec.Body.String()
			if strings.Contains(body, iacGrantTestGrantedARN) || strings.Contains(body, iacGrantTestOtherARN) {
				t.Fatalf("response leaked a finding for an empty-grant caller: %s", body)
			}
		})
	}
}

// TestIaCManagementStatusRoutesEnforceScopeGrant covers get_iac_management_status
// and explain_iac_management_status: exact single-ARN lookups where the sharper
// attack is a caller who already knows the out-of-grant resource's ARN and
// requests it directly. Both routes must return "no finding" for a granted
// account_id + an out-of-grant ARN, and the real finding only for the granted
// ARN.
func TestIaCManagementStatusRoutesEnforceScopeGrant(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/api/v0/iac/management-status",
		"/api/v0/iac/management-status/explain",
	} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			t.Run("out_of_grant_arn_returns_no_finding", func(t *testing.T) {
				t.Parallel()
				store := fakeIaCManagementStore{rows: iacGrantTestFindings()}
				handler := &IaCHandler{Profile: ProfileLocalAuthoritative, Management: store}
				mux := http.NewServeMux()
				handler.Mount(mux)

				auth := iacGrantScopedAuthContext([]string{iacGrantTestGrantedScope})
				body := map[string]any{"account_id": iacGrantTestAccountID, "arn": iacGrantTestOtherARN}
				req := newIaCManagementRouteRequest(t, path, body, &auth)
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, req)

				if got, want := rec.Code, http.StatusOK; got != want {
					t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
				}
				// The response's top-level "arn" field intentionally echoes the
				// caller's own request.ARN regardless of grant outcome (the
				// caller already knows the ARN they asked for), so the leak
				// signal is "finding" (must be nil) and total_findings_count
				// (must be 0, not 1 -- 1 would mean the store matched the
				// out-of-grant row and only the response projection hid it).
				var envelope ResponseEnvelope
				if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
					t.Fatalf("json.Unmarshal() error = %v, want nil", err)
				}
				data := envelope.Data.(map[string]any)
				if data["finding"] != nil {
					t.Fatalf("finding = %#v, want nil for an out-of-grant ARN", data["finding"])
				}
				if got, want := data["total_findings_count"], float64(0); got != want {
					t.Fatalf("total_findings_count = %#v, want %#v -- the out-of-grant row must never match the store read", got, want)
				}
			})

			t.Run("in_grant_arn_returns_finding", func(t *testing.T) {
				t.Parallel()
				store := fakeIaCManagementStore{rows: iacGrantTestFindings()}
				handler := &IaCHandler{Profile: ProfileLocalAuthoritative, Management: store}
				mux := http.NewServeMux()
				handler.Mount(mux)

				auth := iacGrantScopedAuthContext([]string{iacGrantTestGrantedScope})
				body := map[string]any{"account_id": iacGrantTestAccountID, "arn": iacGrantTestGrantedARN}
				req := newIaCManagementRouteRequest(t, path, body, &auth)
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, req)

				if got, want := rec.Code, http.StatusOK; got != want {
					t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
				}
				if !strings.Contains(rec.Body.String(), iacGrantTestGrantedARN) {
					t.Fatalf("response missing granted finding: %s", rec.Body.String())
				}
			})

			t.Run("empty_grant_short_circuits", func(t *testing.T) {
				t.Parallel()
				var dbTouched bool
				store := fakeIaCManagementStore{rows: iacGrantTestFindings(), dbTouched: &dbTouched}
				handler := &IaCHandler{Profile: ProfileLocalAuthoritative, Management: store}
				mux := http.NewServeMux()
				handler.Mount(mux)

				auth := iacGrantScopedAuthContext(nil)
				body := map[string]any{"account_id": iacGrantTestAccountID, "arn": iacGrantTestGrantedARN}
				req := newIaCManagementRouteRequest(t, path, body, &auth)
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, req)

				if got, want := rec.Code, http.StatusOK; got != want {
					t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
				}
				if dbTouched {
					t.Fatal("dbTouched = true, want false for an empty scoped grant")
				}
			})
		})
	}
}

// TestIaCManagementFamilyRoutesUnscopedCallerUnaffected proves an all-scopes
// caller (no AuthContext, matching a shared-key/admin token) sees every
// finding regardless of AWS scope -- the #5167 W4 change must not narrow
// existing shared-key behavior.
func TestIaCManagementFamilyRoutesUnscopedCallerUnaffected(t *testing.T) {
	t.Parallel()

	for _, route := range iacManagementFamilyRoutes() {
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()

			store := fakeIaCManagementStore{rows: iacGrantTestFindings()}
			handler := &IaCHandler{Profile: ProfileLocalAuthoritative, Management: store}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := newIaCManagementRouteRequest(t, route.path, route.body, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			if got, want := iacManagementFamilyTotalFindingsCount(t, rec.Body.Bytes()), 2; got != want {
				t.Fatalf("total_findings_count = %d, want %d (both findings, unscoped caller): %s", got, want, rec.Body.String())
			}
			body := rec.Body.String()
			if route.includesRawARN && (!strings.Contains(body, iacGrantTestGrantedARN) || !strings.Contains(body, iacGrantTestOtherARN)) {
				t.Fatalf("unscoped caller must see every finding, got: %s", body)
			}
		})
	}
}
