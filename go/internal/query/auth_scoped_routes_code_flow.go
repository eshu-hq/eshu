// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "net/http"

// scopedCodeFlowRoute reports whether a code-flow API route is safe for scoped
// tokens. The handlers require repo_id, apply AuthContext repository filtering
// before the store read, and return bounded counts, truncation, ambiguity, and
// unsupported-language states from that already-filtered scope.
func scopedCodeFlowRoute(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	switch r.URL.Path {
	case "/api/v0/code/flow/taint-path",
		"/api/v0/code/flow/reaching-def",
		"/api/v0/code/flow/cfg-summary",
		"/api/v0/code/flow/pdg-summary":
		return true
	default:
		return false
	}
}
