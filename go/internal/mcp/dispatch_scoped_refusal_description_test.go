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
// refusal outcome can be tied to the scoped-token mention it governs.
// Semicolons split: "Supports scoped tokens; rejects malformed selectors"
// must not pass as one statement. Abbreviations such as "e.g." still split
// when followed by whitespace, which errs fail-safe (a genuine refusal using
// one must drop the abbreviation); that behavior is pinned in the regression
// test so it cannot drift silently.
var sentenceBoundaryPattern = regexp.MustCompile(`[.?!;](?:\s+|$)`)

// boundRefusalPattern requires the refusal outcome to apply to the scoped
// caller, not merely to appear near a scoped-token mention (Codex P1 on PR
// #6587: the pre-#6570 analyze sentence names "a scoped token" and "reject
// ... an ungranted repository selector" in one statement while refusing
// scoped callers nothing; owner P2 round 2: the same hole in active voice
// and bare 403 proximity). Only the passive scoped-subject binding qualifies
// ("Scoped ... token ... are refused"): earlier revisions also accepted an
// active-voice object branch and 403-proximity branches, but a probe over
// every checked refusal sentence showed all of them satisfy the passive
// branch while the extra branches admitted unbound outcomes (reviewer P2def
// round 3: verb-near-403 firing far from the mention in comma-joined text),
// so they were removed rather than documented. Future active-voice refusal
// prose fails closed here and must use the precedent passive shape. Bare
// proximity in any direction never qualifies.
var boundRefusalPattern = regexp.MustCompile(`(?i)(scoped[^.?!;]{0,80}?token[^.?!;]{0,80}?(are|is)\s+(refused|rejected))`)

// descriptionDocumentsScopedRefusal reports whether desc documents the
// scoped-token refusal in a single statement: at least one statement must
// bind the refusal outcome to the scoped/personal-token caller. Matching a
// mere mention plus a mere outcome lets them succeed incidentally, which is
// the gap #6572 closes.
func descriptionDocumentsScopedRefusal(desc string) bool {
	for _, sentence := range sentenceBoundaryPattern.Split(desc, -1) {
		if scopedRefusalMentionPattern.MatchString(sentence) &&
			boundRefusalPattern.MatchString(sentence) {
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

	// Codex P1 on PR #6587: the exact pre-#6570 analyze_code_relationships
	// sentence names a scoped token and a rejection in one statement without
	// refusing scoped callers anything. It satisfied the old patterns, and
	// same-statement co-occurrence alone still greens it: the outcome must be
	// bound to the scoped caller, not merely nearby.
	motivating := "The relationship-story and call-chain query types return only granted repositories for a scoped token and reject an ungranted repository selector."
	if descriptionDocumentsScopedRefusal(motivating) {
		t.Errorf("motivating pre-fix sentence must not satisfy the guard: %q", motivating)
	}

	// Owner P2 on PR #6587: a semicolon must not smuggle the same shape
	// through. "Supports scoped tokens; rejects malformed selectors" stays
	// one statement unless semicolons split, and documents no refusal.
	incidentalSemi := "Supports scoped tokens; rejects malformed selectors."
	if descriptionDocumentsScopedRefusal(incidentalSemi) {
		t.Errorf("semicolon-joined incidental text must not satisfy the guard: %q", incidentalSemi)
	}

	// Owner P2 on PR #6587, fail-safe pin: abbreviations still split, so a
	// genuine refusal carrying "(e.g. ...)" mid-sentence fails closed and
	// must drop the abbreviation. Locked here so the behavior cannot drift
	// silently in either direction.
	abbreviated := "Scoped tokens, e.g. browser tokens, are rejected with 403."
	if descriptionDocumentsScopedRefusal(abbreviated) {
		t.Errorf("abbreviation-split text must stay fail-safe: %q", abbreviated)
	}

	// Owner P2 round 2 on PR #6587: proximity is not binding, in active
	// voice or around a bare 403. Both sentences refuse something other
	// than the scoped caller while naming a scoped token in one statement.
	for _, desc := range []string{
		"The endpoint rejects an ungranted repository selector for a scoped token holder.",
		"Scoped token users seeing 403 should retry.",
	} {
		if descriptionDocumentsScopedRefusal(desc) {
			t.Errorf("unbound outcome must not satisfy the guard: %q", desc)
		}
	}
}
