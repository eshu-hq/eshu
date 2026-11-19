// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reachabilitystore

import (
	"strings"
	"testing"
)

// TestIntentCursorProbeSQLStaysIndexFriendly pins the #10870 idle-probe
// statement shape: one backward LIMIT-1 scan per traversed domain over the
// existing (projection_domain, completed_at, created_at) pending index, no
// bind parameters to drift the generic plan, and exactly the two traversed
// domains the CompleteRunGateSQL admits.
func TestIntentCursorProbeSQLStaysIndexFriendly(t *testing.T) {
	t.Parallel()

	query := codeReachabilityIntentCursorSQL
	for _, want := range []string{
		"projection_domain = 'code_calls'",
		"projection_domain = 'inheritance_edges'",
		"completed_at IS NOT NULL",
		"ORDER BY completed_at DESC LIMIT 1",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("cursor probe SQL missing %q:\n%s", want, query)
		}
	}
	if got := strings.Count(query, "ORDER BY completed_at DESC LIMIT 1"); got != 2 {
		t.Fatalf("cursor probe SQL has %d per-domain LIMIT-1 scans, want 2:\n%s", got, query)
	}
	if strings.Contains(query, "$") {
		t.Fatalf("cursor probe SQL must not take bind parameters (plan must be limit-shape stable):\n%s", query)
	}
	if strings.Contains(query, "code_reachability_repository_watermarks") {
		t.Fatalf("cursor probe SQL must not touch the watermark table (O(1) probe, not a join):\n%s", query)
	}
}

// TestIntentCursorProbeSQLGuardCatchesDrift is the seeded-violation half of
// the #10870 probe-shape guard: the same assertions, run against deliberately
// degraded statements, must fire, proving the guard above is not vacuous.
func TestIntentCursorProbeSQLGuardCatchesDrift(t *testing.T) {
	t.Parallel()

	check := func(query string) bool {
		if strings.Count(query, "ORDER BY completed_at DESC LIMIT 1") != 2 {
			return false
		}
		if strings.Contains(query, "$") {
			return false
		}
		if strings.Contains(query, "code_reachability_repository_watermarks") {
			return false
		}
		for _, want := range []string{
			"projection_domain = 'code_calls'",
			"projection_domain = 'inheritance_edges'",
			"completed_at IS NOT NULL",
		} {
			if !strings.Contains(query, want) {
				return false
			}
		}
		return true
	}

	if !check(codeReachabilityIntentCursorSQL) {
		t.Fatal("guard rejects the shipped cursor probe SQL")
	}
	// A forward scan without LIMIT, a parameterized domain, and a watermark
	// join must each trip the guard.
	for name, degraded := range map[string]string{
		"unbounded aggregate": `SELECT max(completed_at) FROM shared_projection_intents WHERE projection_domain = 'code_calls'`,
		"parameterized":       `SELECT completed_at FROM shared_projection_intents WHERE projection_domain = $1 AND completed_at IS NOT NULL ORDER BY completed_at DESC LIMIT 1`,
		"watermark join":      codeReachabilityIntentCursorSQL + ` JOIN code_reachability_repository_watermarks USING (scope_id)`,
	} {
		if check(degraded) {
			t.Fatalf("guard accepts degraded %q statement", name)
		}
	}
}
