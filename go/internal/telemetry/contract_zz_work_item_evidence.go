// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import "slices"

// SpanQueryWorkItemEvidence wraps source-only work-item evidence reads from
// active Jira/work-item facts.
const SpanQueryWorkItemEvidence = "query.work_item_evidence"

const (
	// SpanAttrWorkItemEvidenceQueryCount records one bounded work-item evidence
	// query on the route span.
	SpanAttrWorkItemEvidenceQueryCount = "eshu.query_count"
	// SpanAttrWorkItemEvidenceResultCount records returned work-item evidence
	// rows after page truncation.
	SpanAttrWorkItemEvidenceResultCount = "eshu.result_count"
	// SpanAttrWorkItemEvidenceStaleCount records rows labeled stale evidence.
	SpanAttrWorkItemEvidenceStaleCount = "eshu.stale_evidence_count"
	// SpanAttrWorkItemEvidencePermissionHiddenCount records rows hidden by
	// Jira permissions or issue security.
	SpanAttrWorkItemEvidencePermissionHiddenCount = "eshu.permission_hidden_count"
	// SpanAttrWorkItemEvidenceRejectedUnsafePayloadCount records rows labeled
	// rejected unsafe payload evidence.
	SpanAttrWorkItemEvidenceRejectedUnsafePayloadCount = "eshu.rejected_unsafe_payload_count"
	// SpanAttrWorkItemEvidenceUnsupportedLinkTypeCount records rows with
	// unsupported remote-link evidence.
	SpanAttrWorkItemEvidenceUnsupportedLinkTypeCount = "eshu.unsupported_link_type_count"
	// SpanAttrWorkItemEvidenceMetadataWarningCount records rows labeled
	// metadata-warning evidence: a metadata collection that was blocked
	// (archived, unsupported, or permission-hidden) rather than an ordinary
	// provider fact. An operator watching the route span sees warning volume
	// distinct from the record-level permission-hidden count.
	SpanAttrWorkItemEvidenceMetadataWarningCount = "eshu.metadata_warning_count"
	// SpanAttrWorkItemEvidenceMissingCount records one missing-evidence result
	// when a scoped read returns zero rows.
	SpanAttrWorkItemEvidenceMissingCount = "eshu.missing_evidence_count"
	// SpanAttrWorkItemEvidenceTruncated records whether limit+1 pagination found
	// another work-item evidence page.
	SpanAttrWorkItemEvidenceTruncated = "eshu.truncated"
)

func init() {
	for idx, name := range spanNames {
		if name == SpanQueryIncidentContext {
			spanNames = slices.Insert(spanNames, idx+1, SpanQueryWorkItemEvidence)
			return
		}
	}
	spanNames = append(spanNames, SpanQueryWorkItemEvidence)
}
