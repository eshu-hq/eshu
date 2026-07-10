// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
)

// TestEveryAdminAuthRouteIsScopedAllowlisted is the root-cause guard for
// #5004/#4966: both issues shipped a fully implemented, handler-wired
// /api/v0/auth/admin/* route family (sign-in-policy, then provider-configs)
// that nobody added to scopedHTTPRouteSupportsTenantFilter, so every real
// browser-session admin got the scoped-authorization 403 before the
// handler's own AllScopes check ever ran. Two audits found this by hand;
// this test finds it automatically going forward.
//
// The route set is derived from the generated surface inventory
// (capabilitycatalog.LoadSurfaceInventory, embedded from
// data/surface-inventory.generated.json) rather than hand-maintained here,
// for the same reason internal/ask/catalog/planner_exclusions_test.go derives
// its completeness gate from the same artifact: a golden drift test
// (cmd/capability-inventory) already keeps that inventory in lockstep with
// every live api_route, so a new admin route landing without a matching
// allowlist entry fails THIS test the moment its route lands and the
// inventory is regenerated — no separate hand-maintained route list to forget
// to update.
func TestEveryAdminAuthRouteIsScopedAllowlisted(t *testing.T) {
	t.Parallel()

	const adminRoutePrefix = "/api/v0/auth/admin/"

	inventory, err := capabilitycatalog.LoadSurfaceInventory()
	if err != nil {
		t.Fatalf("load surface inventory: %v", err)
	}

	var checked, missing []string
	for _, surface := range inventory.Surfaces {
		if surface.Category != capabilitycatalog.SurfaceAPIRoute || surface.Readiness != capabilitycatalog.ReadinessImplemented {
			continue
		}
		method, path, ok := strings.Cut(surface.Name, " ")
		if !ok || !strings.HasPrefix(path, adminRoutePrefix) {
			continue
		}
		checked = append(checked, surface.Name)
		req := httptest.NewRequest(method, path, nil)
		if !scopedHTTPRouteSupportsTenantFilter(req) {
			missing = append(missing, surface.Name)
		}
	}

	// An empty checked set means the inventory filter itself broke (e.g. the
	// admin route prefix or category/readiness values drifted) rather than
	// there being no admin routes — fail loudly instead of passing vacuously.
	if len(checked) == 0 {
		t.Fatal("found zero implemented /api/v0/auth/admin/* routes in the surface inventory; " +
			"the inventory filter is broken (this gate must never pass vacuously)")
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("%d implemented admin auth route(s) are missing from the browser-session scoped-route "+
			"allowlist — a real admin's cookie-session request will get a 403 before the handler's own "+
			"AllScopes check ever runs (the #5004/#4966 bug class); add each to scopedAuthAdminReadRoute or "+
			"scopedAuthAdminMutationRoute in auth_scoped_routes.go:", len(missing))
		for _, name := range missing {
			t.Errorf("  %s", name)
		}
	}
}
