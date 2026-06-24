// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "github.com/eshu-hq/eshu/go/internal/facts"

// sourceACLStateResponseKey is the wire field name used to surface the bounded
// source-ACL-state observation on content, documentation-evidence, and
// semantic-evidence readbacks. It is a distinct access-posture axis kept
// separate from the freshness/truth fields (#2138 truth-label taxonomy); it is
// never folded into freshness_state, truth_level, or the binary
// permission-denied decision.
const sourceACLStateResponseKey = "source_acl_state"

// boundedSourceACLState returns the collector-observed bounded source-ACL-state
// (allowed|denied|partial|missing|stale) carried verbatim on a fact payload's
// acl_summary, or the empty string when there is no bounded ACL claim to
// surface.
//
// It reads payload.acl_summary.source_acl_state and validates it against
// facts.ValidSourceACLState. It fails closed: an absent, empty, or non-bounded
// value yields the empty string so a corrupt or future value can never surface
// as an authoritative ACL claim. This helper only EXPOSES the value the reducer
// and collector already carry; it never upgrades a non-allowed observation to
// allowed, never folds the value into freshness, and never synthesizes a
// default the collector did not assert. Choosing a conservative default for an
// unobserved source, and changing access decisions based on this value
// (fail-closed enforcement, denied-vs-missing disclosure), are reserved for the
// query disclosure policy and security review (#2164); this helper performs no
// enforcement and alters no returned rows.
func boundedSourceACLState(payload map[string]any) string {
	state := nestedString(payload, "acl_summary", sourceACLStateResponseKey)
	if !facts.ValidSourceACLState(state) {
		return ""
	}
	return state
}

// surfaceSourceACLState copies the bounded source-ACL-state observation from the
// fact payload onto the readback response row under sourceACLStateResponseKey,
// only when the collector asserted a bounded value. It is additive metadata: it
// adds no row, removes no row, and changes no other field. When the payload
// carries no bounded ACL claim the field is omitted entirely (absence means "no
// ACL claim").
func surfaceSourceACLState(out, payload map[string]any) {
	if state := boundedSourceACLState(payload); state != "" {
		out[sourceACLStateResponseKey] = state
	}
}
