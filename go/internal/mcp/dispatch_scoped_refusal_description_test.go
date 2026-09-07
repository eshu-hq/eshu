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

// sentenceBoundaryPattern splits a tool description into statements so the
// refusal outcome can be tied to the scoped-token mention it governs. Only
// terminal punctuation followed by whitespace (or the end of the string)
// splits: abbreviations such as "e.g." keep their clause attached to the
// words that follow, and semicolons stay inside the statement they join.
var sentenceBoundaryPattern = regexp.MustCompile(`[.?!](?:\s+|$)`)

// descriptionDocumentsScopedRefusal reports whether desc documents the
// scoped-token refusal in a single statement: at least one sentence must both
// name the scoped/personal-token caller shape and state the refusal outcome.
// Matching the two patterns against the whole description at once lets them
// succeed incidentally in unrelated sentences, which is the gap #6572 closes.
func descriptionDocumentsScopedRefusal(desc string) bool {
	for _, sentence := range sentenceBoundaryPattern.Split(desc, -1) {
		if scopedRefusalMentionPattern.MatchString(sentence) &&
			scopedRefusalOutcomePattern.MatchString(sentence) {
			return true
		}
	}
	return false
}

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

		if !descriptionDocumentsScopedRefusal(tool.Description) {
			t.Errorf(
				"tool %q dispatches to %s %s, which refuses scoped-token callers, "+
					"but its description documents no scoped-token refusal: %q",
				tool.Name, route.method, route.path, tool.Description,
			)
		}
	}
}

// TestDescriptionDocumentsScopedRefusalRequiresSameStatement is the #6572
// regression: the pre-fix analyze_code_relationships text mentioned a scoped
// token in one sentence and a rejection in another without documenting any
// refusal, yet satisfied both description patterns incidentally. The guard
// must tie the outcome to the mention in a single statement.
func TestDescriptionDocumentsScopedRefusalRequiresSameStatement(t *testing.T) {
	t.Parallel()

	incidental := "Look up related code for a scoped token holder. " +
		"The query planner may reject an ungranted repository selector at plan time."
	if !scopedRefusalMentionPattern.MatchString(incidental) ||
		!scopedRefusalOutcomePattern.MatchString(incidental) {
		t.Fatalf("regression case must satisfy the old incidental patterns: %q", incidental)
	}
	if descriptionDocumentsScopedRefusal(incidental) {
		t.Errorf("description without a same-statement refusal must not satisfy the guard: %q", incidental)
	}

	for _, desc := range []string{
		"A scoped or browser-session token is rejected with a 403 on this route.",
		"Scoped tokens are refused with a 403 because the route cannot be tenant-filtered yet.",
	} {
		if !descriptionDocumentsScopedRefusal(desc) {
			t.Errorf("genuine refusal statement must satisfy the guard: %q", desc)
		}
	}
}
