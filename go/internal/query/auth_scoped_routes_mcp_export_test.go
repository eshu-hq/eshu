// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

func TestHTTPOnlySharedKeyRoutesStayOutOfMCPStalenessLedger(t *testing.T) {
	t.Parallel()

	exported := make(map[string]struct{})
	for _, surface := range SharedKeyOnlyRouteSurfaces() {
		exported[surface] = struct{}{}
	}
	for surface := range httpOnlySharedKeyOnlyRoutes {
		if _, ledgered := sharedKeyOnlyRoutes[surface]; !ledgered {
			t.Errorf("HTTP-only shared-key route %q is missing from the authorization ledger", surface)
		}
		if _, leaked := exported[surface]; leaked {
			t.Errorf("HTTP-only shared-key route %q leaked into the MCP staleness ledger", surface)
		}
	}
}
