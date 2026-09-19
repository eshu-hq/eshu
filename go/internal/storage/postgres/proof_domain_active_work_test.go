// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"encoding/json"
	"time"
)

// proofActiveWorkSummaryRows renders the proof harness's stage counts, domain
// backlog, and queue snapshot as activeWorkSummaryQuery section rows
// (section, ordinal, JSON object keyed by the standalone reads' column names).
// The harness has no blockage or failure rows.
func proofActiveWorkSummaryRows(workItems map[string]proofWorkItem, asOf time.Time) ([][]any, error) {
	var out [][]any
	appendSection := func(section string, columns []string, rows [][]any) error {
		for i, row := range rows {
			object := make(map[string]any, len(columns))
			for c, name := range columns {
				object[name] = row[c]
			}
			encoded, err := json.Marshal(object)
			if err != nil {
				return err
			}
			out = append(out, []any{section, int64(i + 1), string(encoded)})
		}
		return nil
	}
	if err := appendSection(activeWorkSectionStage, []string{"stage", "status", "count"},
		proofStageCountRows(workItems)); err != nil {
		return nil, err
	}
	if err := appendSection(activeWorkSectionBacklog, []string{
		"domain", "outstanding_count", "in_flight_count", "retrying_count",
		"dead_letter_count", "failed_count", "oldest_outstanding_age_seconds",
	}, proofDomainBacklogRows(workItems, asOf)); err != nil {
		return nil, err
	}
	if err := appendSection(activeWorkSectionQueue, []string{
		"total_count", "outstanding_count", "pending_count", "in_flight_count",
		"retrying_count", "succeeded_count", "dead_letter_count", "failed_count",
		"provenance_edge_identity_upgrade_applied", "provenance_edge_identity_upgrade_required",
		"oldest_outstanding_age_seconds", "overdue_claim_count",
	}, [][]any{proofQueueSnapshotRow(workItems, asOf)}); err != nil {
		return nil, err
	}
	return out, nil
}
