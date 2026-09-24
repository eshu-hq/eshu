// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // #6818 move 4b: root auth surface (aliases, forwarders, route policies, handler wiring) stays in package query; moving it into auth/ strands root handler callers and turns this rename into a root-surface relocation, which is a separate follow-up.

import "net/http"

// scopedCapabilityCatalogRoute reports whether the request targets the capability
// catalog read. The catalog is the static, embedded artifact and carries no
// tenant-scoped data, so scoped tokens may read it unfiltered.
func scopedCapabilityCatalogRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/capabilities"
}

// scopedSurfaceInventoryRoute reports whether the request targets the static
// surface inventory. Like the capability catalog, this route serves an embedded
// generated artifact and carries no tenant-scoped data.
func scopedSurfaceInventoryRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/surface-inventory"
}
