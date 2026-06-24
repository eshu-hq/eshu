// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "net/http"

// Scoped-token authorization helpers for the source-only work-item evidence
// read route (GET /api/v0/work-items/evidence).
//
// Work-item facts key on the provider project scope (scope_id, project_key,
// work_item_key), not a git repository. The durable join is the
// linked_repository_id the Jira collector resolves from a confidently typed
// GitHub PR or GitLab MR link before redaction (#2160). Scoped enforcement is
// layered on as (a) an empty-grant short-circuit that returns the bounded
// zero-evidence page without a store read, and (b) the SQL grant predicate
// (listWorkItemEvidenceQuery $9) that intersects each fact's
// linked_repository_id with the grant set. A work item with no durable link —
// every fact kind except a canonicalized external_link, or an out-of-grant
// project selector — stays invisible to scoped tokens (fail-closed), never a
// provider-scope leak. Shared, admin, and local callers carry no grant set and
// keep the unscoped read path.

// writeEmptyWorkItemEvidencePage returns the bounded zero-evidence page for an
// empty-grant scoped token without reading the work-item evidence store. The
// shape mirrors the populated list response so a scoped caller cannot
// distinguish an empty grant from a genuinely empty work-item corpus.
func (h *WorkItemHandler) writeEmptyWorkItemEvidencePage(
	w http.ResponseWriter,
	r *http.Request,
	limit int,
) {
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"evidence":         []WorkItemEvidenceRow{},
		"count":            0,
		"limit":            limit,
		"truncated":        false,
		"missing_evidence": true,
		"states":           []string{WorkItemEvidenceStateMissingEvidence},
	}, BuildTruthEnvelope(
		h.profile(),
		workItemEvidenceCapability,
		TruthBasisSemanticFacts,
		"scoped token grants authorize no repositories; no work-item evidence is attributable to a granted repository link",
	))
}
