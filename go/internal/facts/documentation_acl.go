// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "github.com/eshu-hq/eshu/go/internal/semanticpolicy"

// Bounded source-ACL-state vocabulary for documentation content/evidence facts.
//
// These mirror the semantic-extraction ACL vocabulary in
// [semanticpolicy] so collectors that observe source access posture and the
// semantic-extraction policy gate speak one bounded language. The field is
// additive and optional: a collector that observes no ACL signal MUST omit it
// entirely (absence means "no ACL claim"), and a denied, partial, missing, or
// stale observation MUST NOT be upgraded to allowed.
const (
	// SourceACLStateAllowed records that the source ACL was evaluated and
	// permits the observed read. Only a real ACL evaluation may assert this;
	// a successful read whose restrictions were not collected is partial.
	SourceACLStateAllowed = semanticpolicy.ACLAllowed
	// SourceACLStateDenied records an observed permission-denied or 403 read.
	SourceACLStateDenied = semanticpolicy.ACLDenied
	// SourceACLStatePartial records an incomplete or restricted ACL read that
	// must fail closed rather than be treated as allowed.
	SourceACLStatePartial = semanticpolicy.ACLPartial
	// SourceACLStateMissing records that the source was not found, deleted, or
	// trashed at the origin.
	SourceACLStateMissing = semanticpolicy.ACLMissing
	// SourceACLStateStale records a permitted but stale source revision.
	SourceACLStateStale = semanticpolicy.ACLStale
)

// BoundedSourceACLState returns the bounded source-ACL-state carried on a
// document or source acl_summary so a collector can propagate it verbatim onto
// the derived documentation evidence facts (mention, claim, observation) for
// #2178. It returns the empty string when summary is nil or carries no bounded
// ACL claim, so an unobserved or non-bounded posture is omitted from the
// evidence fact rather than defaulted. It is factual propagation only: it copies
// the observed state verbatim, never upgrades a denied, partial, missing, or
// stale observation to allowed, and never synthesizes a value the source did
// not assert.
func BoundedSourceACLState(summary *DocumentationACLSummary) string {
	if summary == nil {
		return ""
	}
	if !ValidSourceACLState(summary.SourceACLState) {
		return ""
	}
	return summary.SourceACLState
}

// ValidSourceACLState reports whether value is one of the bounded
// source-ACL-state constants. The empty string is not valid here; callers that
// observe no ACL signal omit the field rather than store an empty value.
func ValidSourceACLState(value string) bool {
	switch value {
	case SourceACLStateAllowed,
		SourceACLStateDenied,
		SourceACLStatePartial,
		SourceACLStateMissing,
		SourceACLStateStale:
		return true
	default:
		return false
	}
}
