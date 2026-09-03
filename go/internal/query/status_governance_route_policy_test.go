// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"testing"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// TestGovernanceStatusReadbackAgreesWithAllScopeAdmission pins the two
// surfaces ESHU_GOVERNANCE_MODE drives to one another: the admission decision
// ScopedRoutePolicyForGovernanceMode makes for an all-scope bearer on a
// grant-bound route, and what GET /api/v0/status/governance tells an operator
// about that same decision.
//
// The codex PR #6497 finding they were failing on: an unrecognized non-empty
// mode (the realistic shape is a hyphen typo, "hosted-multi-tenant") is
// fail-closed in admission, but the readback ran the same value through
// normalizeGovernanceConfig, which rewrote it to "local_no_policy" -- so the
// operator debugging a wave of 403s read the permissive local posture back
// and had no way to see either the typo or the refusal. A readback that
// contradicts the running policy is worse than no readback: it sends the
// operator looking for the fault somewhere else.
//
// The expected admission is derived FROM the payload rather than restated
// beside it, so the test fails whenever the two disagree, in either
// direction, for any mode added to the table later. wantMode pins the readback
// string itself, so the pair cannot agree by both going wrong.
func TestGovernanceStatusReadbackAgreesWithAllScopeAdmission(t *testing.T) {
	t.Parallel()

	tenantBound := AuthContext{
		Mode:        AuthModeScoped,
		TenantID:    "tenant_a",
		WorkspaceID: "workspace_a",
		AllScopes:   true,
	}

	for _, tc := range []struct {
		name string
		// mode is the raw ESHU_GOVERNANCE_MODE value, handed to both surfaces
		// exactly as cmd/api and cmd/mcp-server hand it to them.
		mode       string
		wantMode   string
		wantPolicy string
	}{
		{name: "unset reads as the local default", mode: "", wantMode: "local_no_policy", wantPolicy: "admitted"},
		{name: "local_no_policy", mode: "local_no_policy", wantMode: "local_no_policy", wantPolicy: "admitted"},
		{name: "hosted_single_tenant", mode: "hosted_single_tenant", wantMode: "hosted_single_tenant", wantPolicy: "admitted"},
		{name: "hosted_multi_tenant", mode: "hosted_multi_tenant", wantMode: "hosted_multi_tenant", wantPolicy: "refused"},
		{name: "a hyphen typo is reported, not rewritten", mode: "hosted-multi-tenant", wantMode: "unrecognized", wantPolicy: "refused"},
		{name: "a case mismatch is reported, not rewritten", mode: "HOSTED_SINGLE_TENANT", wantMode: "unrecognized", wantPolicy: "refused"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			config := GovernanceStatusConfig{Mode: tc.mode}
			payload := buildGovernanceStatus(config, statuspkg.SemanticExtractionStatus{})

			if got := payload["mode"]; got != tc.wantMode {
				t.Fatalf("mode = %#v, want %#v", got, tc.wantMode)
			}
			section, ok := payload["all_scope_route_policy"].(map[string]any)
			if !ok {
				t.Fatalf("all_scope_route_policy = %#v, want a map", payload["all_scope_route_policy"])
			}
			advertised, ok := section["grant_bound_routes"].(string)
			if !ok {
				t.Fatalf("all_scope_route_policy.grant_bound_routes = %#v, want a string", section["grant_bound_routes"])
			}
			if advertised != tc.wantPolicy {
				t.Fatalf("all_scope_route_policy.grant_bound_routes = %q, want %q", advertised, tc.wantPolicy)
			}

			wantStatus, wantReason := http.StatusForbidden, scopedRouteAllScopeGrantRequiredReason
			if advertised == "admitted" {
				wantStatus, wantReason = http.StatusOK, ""
			}
			assertBearerFreshnessDeltaRoute(
				t, tenantBound, ScopedRoutePolicyForGovernanceMode(config),
				"/api/v0/freshness/changed-since", wantStatus, wantReason,
			)
		})
	}
}

// TestGovernanceStatusUnrecognizedModeCarriesReason proves the readback says
// why it is fail-closed in the channel operators already read for remedies,
// rather than leaving "unrecognized" to be interpreted. The raw configured
// value is deliberately not echoed: this route holds a redaction contract
// (TestGovernanceStatusConfigDropsUnsafeStatusValues) that no operator-supplied
// string reaches the response, and the actionable fact -- the configured mode
// is not one of supported_modes and all-scope callers are being refused -- is
// carried without it.
func TestGovernanceStatusUnrecognizedModeCarriesReason(t *testing.T) {
	t.Parallel()

	payload := buildGovernanceStatus(
		GovernanceStatusConfig{Mode: "hosted-multi-tenant"},
		statuspkg.SemanticExtractionStatus{},
	)
	requireStringSliceContains(t, payload["reasons"], "governance_mode_unrecognized")
}
