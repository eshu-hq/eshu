// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "github.com/eshu-hq/eshu/go/internal/facts"

// workItemEvidenceFactKinds bounds the evidence read to the work_item family
// the fact-kind registry maps to GET /api/v0/work-items/evidence, MINUS
// work_item.metadata_warning (see workItemEvidenceReadKinds). Deriving it from
// facts.WorkItemFactKinds() keeps the SQL kind list, the decode switch in
// decodeWorkItemEvidenceRow, and the registry in lockstep so a future family
// addition trips the drift guard instead of silently narrowing the read.
var workItemEvidenceFactKinds = workItemEvidenceReadKinds()

// workItemEvidenceReadKinds returns the work_item fact kinds the evidence read
// surface serves: the whole registered family from facts.WorkItemFactKinds()
// except work_item.metadata_warning. The metadata_warning kind is excluded on
// purpose: WorkItemEvidenceRow is a public wire shape with no metadata_type or
// reason field, so surfacing a collection warning through it would strip the
// warning's own contract fields (metadata_type, reason) and present it as an
// ordinary provider fact — misleading truth on a public surface. Surfacing it
// correctly is a wire + semantics change tracked in #4887; until then the kind
// stays off the read set (it was never surfaced before, so excluding it is
// regression-free). Its decode branch and wrapper are kept forward-ready for
// that follow-up.
func workItemEvidenceReadKinds() []string {
	all := facts.WorkItemFactKinds()
	kinds := make([]string, 0, len(all))
	for _, kind := range all {
		if kind == facts.WorkItemMetadataWarningFactKind {
			continue
		}
		kinds = append(kinds, kind)
	}
	return kinds
}
