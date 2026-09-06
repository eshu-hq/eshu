// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// scopedRefusalMentionPattern requires a tool description to name the
// scoped/personal-token caller shape ...
var scopedRefusalMentionPattern = regexp.MustCompile(`(?i)scoped.{0,40}token`)

// scopedRefusalOutcomePattern requires the description to state the refusal
// outcome, not merely to mention tokens in passing.
var scopedRefusalOutcomePattern = regexp.MustCompile(`(?i)(403|refus|reject)`)

// TestFailClosedToolsDocumentScopedRefusal is the #5167 "no silent 403s"
// contract: every MCP tool whose dispatched route fails closed for
// scoped/personal-token callers -- the Group B pending-row-filtering ledger
// and the Group C shared-key-only ledger -- must say so in its own
// description, with the reason the route cannot be tenant-filtered yet.
// A personal-token caller otherwise discovers the 403 only by calling,
// which is exactly the asymmetry #5167 exists to close. The precedents are
// execute_cypher_query ("A scoped or browser-session token is rejected ...")
// and get_service_changed_since ("Scoped tokens ... are refused with a
// 403 ... (#6475)").
func TestFailClosedToolsDocumentScopedRefusal(t *testing.T) {
	t.Parallel()

	for _, tool := range ReadOnlyTools() {
		args := minimalDispatchRouteArgs(tool.Name)
		route, err := resolveRoute(tool.Name, args)
		if err != nil {
			t.Fatalf("tool %q is registered but has no dispatch route: %v", tool.Name, err)
		}

		req := httptest.NewRequest(route.method, route.path, nil)
		failClosed := query.IsPendingRowFilteringRoute(req) || query.IsSharedKeyOnlyRoute(req)
		if !failClosed {
			continue
		}

		if !scopedRefusalMentionPattern.MatchString(tool.Description) ||
			!scopedRefusalOutcomePattern.MatchString(tool.Description) {
			t.Errorf(
				"tool %q dispatches to %s %s, which refuses scoped-token callers, "+
					"but its description documents no scoped-token refusal: %q",
				tool.Name, route.method, route.path, tool.Description,
			)
		}
	}
}
