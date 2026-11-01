// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // #6818 move 4b: root auth surface (aliases, forwarders, route policies, handler wiring) stays in package query; moving it into auth/ strands root handler callers and turns this rename into a root-surface relocation, which is a separate follow-up.

import "net/http"

// scopedCapabilityCatalogRoute reports whether the request targets the capability
// catalog read. The catalog is the static, embedded artifact and carries no
// tenant-scoped data, so scoped tokens may read it unfiltered.
//
// The eshu-mcp satellite depends on this classification (eshu-hq/eshu-mcp#50):
// it probes GET /api/v0/capabilities to decide whether core admits a forwarded
// credential, and expects 200 for every valid credential class and 401 for an
// invalid one. It uses this route because /api/v0/auth/profile answers 401 for
// a valid token that carries no subject hash. Pulling this route off the
// allowlist or out of the no-caller-grant class, or gating it on a permission
// feature, breaks that check. TestCapabilitiesProbe* in
// go/cmd/api/capabilities_probe_contract_test.go pins it; change them together
// with the satellite.
func scopedCapabilityCatalogRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/capabilities"
}

// scopedSurfaceInventoryRoute reports whether the request targets the static
// surface inventory. Like the capability catalog, this route serves an embedded
// generated artifact and carries no tenant-scoped data.
func scopedSurfaceInventoryRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/surface-inventory"
}
