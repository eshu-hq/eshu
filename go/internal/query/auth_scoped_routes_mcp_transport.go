// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "net/http"

// scopedMCPTransportRoute reports whether the request targets the MCP
// transport endpoints (issue #5168): GET /sse establishes a session, and
// POST /mcp/message dispatches every JSON-RPC method (initialize,
// tools/list, tools/call, ping, notifications/initialized).
// initialize/tools/list/ping/session-establishment return only static server
// info and the tool catalog -- never tenant data -- so they need no
// per-tenant filtering here. tools/call's actual data access is enforced
// transitively exactly like POST /api/v0/ask (see
// scopedHTTPRouteSupportsTenantFilter): go/internal/mcp's dispatchTool
// re-dispatches through this SAME auth middleware against the specific
// underlying /api/v0/... route, so a scoped caller can only reach routes
// that are themselves in this allowlist. Without this entry a
// scoped-token-only deployment could not even call initialize/tools/list --
// the entire MCP transport would be unusable for scoped-token callers, even
// though tools/call already worked for them on any allowlisted route before
// this transport-level check existed.
func scopedMCPTransportRoute(r *http.Request) bool {
	if r.URL.Path == "/sse" && r.Method == http.MethodGet {
		return true
	}
	return r.URL.Path == "/mcp/message" && r.Method == http.MethodPost
}
