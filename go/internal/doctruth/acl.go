// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package doctruth

import "github.com/eshu-hq/eshu/go/internal/facts"

// evidenceACLSummary builds the bounded acl_summary to attach to a derived
// documentation evidence fact (mention or claim) from the section's observed
// source access posture.
//
// It returns nil when the section carries no bounded source_acl_state, so the
// acl_summary is omitted entirely (absence means "no ACL claim"). A non-bounded
// value is treated as no claim rather than surfaced, so a corrupt or future
// value can never propagate as an authoritative ACL claim. This is factual
// propagation only: it copies the observed posture verbatim, never upgrades a
// denied, partial, missing, or stale observation to allowed, and never
// synthesizes a default the source did not assert.
func evidenceACLSummary(sourceACLState string) *facts.DocumentationACLSummary {
	if !facts.ValidSourceACLState(sourceACLState) {
		return nil
	}
	return &facts.DocumentationACLSummary{SourceACLState: sourceACLState}
}
