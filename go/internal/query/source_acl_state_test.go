// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestBoundedSourceACLStateReturnsObservedState proves every bounded
// source-ACL-state the collector emits on a fact payload's acl_summary is
// returned verbatim, with no upgrade or rewrite.
func TestBoundedSourceACLStateReturnsObservedState(t *testing.T) {
	t.Parallel()

	for _, state := range []string{
		facts.SourceACLStateAllowed,
		facts.SourceACLStateDenied,
		facts.SourceACLStatePartial,
		facts.SourceACLStateMissing,
		facts.SourceACLStateStale,
	} {
		state := state
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			payload := map[string]any{
				"acl_summary": map[string]any{"source_acl_state": state},
			}
			if got := boundedSourceACLState(payload); got != state {
				t.Fatalf("boundedSourceACLState = %q, want %q (verbatim)", got, state)
			}
		})
	}
}

// TestBoundedSourceACLStateOmitsWhenAbsent proves a payload with no acl_summary,
// an empty source_acl_state, or no source_acl_state key yields the empty string:
// absence means "no ACL claim". The reader must never synthesize a default.
func TestBoundedSourceACLStateOmitsWhenAbsent(t *testing.T) {
	t.Parallel()

	cases := map[string]map[string]any{
		"no payload":              {},
		"no acl_summary":          {"freshness_state": facts.SourceACLStateStale},
		"empty acl_summary":       {"acl_summary": map[string]any{}},
		"empty source_acl_state":  {"acl_summary": map[string]any{"source_acl_state": ""}},
		"acl_summary wrong type":  {"acl_summary": "denied"},
		"source_acl_state number": {"acl_summary": map[string]any{"source_acl_state": 3}},
	}
	for name, payload := range cases {
		payload := payload
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := boundedSourceACLState(payload); got != "" {
				t.Fatalf("boundedSourceACLState = %q, want empty (no ACL claim)", got)
			}
		})
	}
}

// TestBoundedSourceACLStateDropsNonBoundedValue proves a corrupt or future
// non-bounded value never surfaces as an authoritative ACL claim (fail closed).
func TestBoundedSourceACLStateDropsNonBoundedValue(t *testing.T) {
	t.Parallel()

	for _, bad := range []string{"unknown", "ALLOWED", "hidden", "permission_denied", "  "} {
		bad := bad
		t.Run(bad, func(t *testing.T) {
			t.Parallel()
			payload := map[string]any{
				"acl_summary": map[string]any{"source_acl_state": bad},
			}
			if got := boundedSourceACLState(payload); got != "" {
				t.Fatalf("boundedSourceACLState(%q) = %q, want empty (non-bounded dropped)", bad, got)
			}
		})
	}
}

// TestSurfaceSourceACLStateIsAdditive proves the helper adds source_acl_state as
// a distinct field only when a bounded value is present, and omits it otherwise,
// without touching any other field on the response row.
func TestSurfaceSourceACLStateIsAdditive(t *testing.T) {
	t.Parallel()

	// Present + bounded: added as a distinct field, distinct from freshness.
	out := map[string]any{"freshness_state": "stale"}
	surfaceSourceACLState(out, map[string]any{
		"freshness_state": "stale",
		"acl_summary":     map[string]any{"source_acl_state": facts.SourceACLStateDenied},
	})
	if got := out["source_acl_state"]; got != facts.SourceACLStateDenied {
		t.Fatalf("out[source_acl_state] = %#v, want %q", got, facts.SourceACLStateDenied)
	}
	// A row can be fresh+denied or stale+allowed: ACL and freshness are
	// independent axes, so surfacing ACL must not overwrite freshness.
	if got := out["freshness_state"]; got != "stale" {
		t.Fatalf("freshness_state mutated to %#v, want unchanged", got)
	}

	// Absent: field omitted entirely.
	omitted := map[string]any{"freshness_state": "fresh"}
	surfaceSourceACLState(omitted, map[string]any{"freshness_state": "fresh"})
	if _, present := omitted["source_acl_state"]; present {
		t.Fatalf("source_acl_state surfaced for payload with no ACL claim: %#v", omitted)
	}
}
