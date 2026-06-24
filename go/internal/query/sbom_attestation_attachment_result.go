// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

func buildSBOMAttestationAttachmentResult(row SBOMAttestationAttachmentRow) SBOMAttestationAttachmentResult {
	warnings, warningCount, warningsTruncated := boundedSBOMWarningSummaries(row.WarningSummaries)
	if row.WarningSummaryCount > warningCount {
		warningCount = row.WarningSummaryCount
	}
	if row.WarningSummariesTruncated || warningCount > len(warnings) {
		warningsTruncated = true
	}
	row.WarningSummaries = warnings
	row.WarningSummaryCount = warningCount
	row.WarningSummariesTruncated = warningsTruncated
	return SBOMAttestationAttachmentResult(row)
}
