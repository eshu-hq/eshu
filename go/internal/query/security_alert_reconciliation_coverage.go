// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "strings"

const (
	securityAlertCoverageComplete         = "complete"
	securityAlertCoverageTargetIncomplete = "target_incomplete"
	securityAlertSourceFreshnessPartial   = "partial"
)

type securityAlertReconciliationCoverageSummary struct {
	State          string `json:"state"`
	PartialRows    int    `json:"partial_rows"`
	RowsConsidered int    `json:"rows_considered"`
}

func securityAlertCoverageForRows(
	rows []SecurityAlertReconciliationResult,
) securityAlertReconciliationCoverageSummary {
	partialRows := 0
	for _, row := range rows {
		if strings.EqualFold(row.SourceFreshness, securityAlertSourceFreshnessPartial) {
			partialRows++
		}
	}
	return securityAlertCoverageFromCounts(len(rows), partialRows)
}

func securityAlertCoverageFromFreshnessCounts(
	totalRows int,
	counts map[string]int,
) securityAlertReconciliationCoverageSummary {
	partialRows := 0
	for freshness, count := range counts {
		if strings.EqualFold(freshness, securityAlertSourceFreshnessPartial) {
			partialRows += count
		}
	}
	return securityAlertCoverageFromCounts(totalRows, partialRows)
}

func securityAlertCoverageFromCounts(
	totalRows int,
	partialRows int,
) securityAlertReconciliationCoverageSummary {
	state := securityAlertCoverageComplete
	if partialRows > 0 {
		state = securityAlertCoverageTargetIncomplete
	}
	return securityAlertReconciliationCoverageSummary{
		State:          state,
		PartialRows:    partialRows,
		RowsConsidered: totalRows,
	}
}
