// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

type collectorBackpressureJSON struct {
	CollectorKind           string           `json:"collector_kind"`
	CollectorInstanceID     string           `json:"collector_instance_id"`
	SourceSystem            string           `json:"source_system"`
	Pending                 int              `json:"pending"`
	Claimed                 int              `json:"claimed"`
	Retrying                int              `json:"retrying"`
	DeadLetter              int              `json:"dead_letter"`
	TerminalFailed          int              `json:"terminal_failed"`
	Expired                 int              `json:"expired"`
	ActiveClaims            int              `json:"active_claims"`
	OverdueClaims           int              `json:"overdue_claims"`
	OldestPendingAge        string           `json:"oldest_pending_age"`
	OldestPendingAgeSeconds float64          `json:"oldest_pending_age_seconds"`
	OldestRetryAge          string           `json:"oldest_retry_age"`
	OldestRetryAgeSeconds   float64          `json:"oldest_retry_age_seconds"`
	OldestClaimAge          string           `json:"oldest_claim_age"`
	OldestClaimAgeSeconds   float64          `json:"oldest_claim_age_seconds"`
	NextRetryDelay          string           `json:"next_retry_delay"`
	NextRetryDelaySeconds   float64          `json:"next_retry_delay_seconds"`
	FailureClassCounts      []namedCountJSON `json:"failure_class_counts,omitempty"`
}

func collectorBackpressureJSONRows(rows []CollectorBackpressureSnapshot) []collectorBackpressureJSON {
	rows = cloneCollectorBackpressure(rows)
	projected := make([]collectorBackpressureJSON, 0, len(rows))
	for _, row := range rows {
		projected = append(projected, collectorBackpressureJSON{
			CollectorKind:           row.CollectorKind,
			CollectorInstanceID:     row.CollectorInstanceID,
			SourceSystem:            row.SourceSystem,
			Pending:                 row.Pending,
			Claimed:                 row.Claimed,
			Retrying:                row.Retrying,
			DeadLetter:              row.DeadLetter,
			TerminalFailed:          row.TerminalFailed,
			Expired:                 row.Expired,
			ActiveClaims:            row.ActiveClaims,
			OverdueClaims:           row.OverdueClaims,
			OldestPendingAge:        row.OldestPendingAge.String(),
			OldestPendingAgeSeconds: row.OldestPendingAge.Seconds(),
			OldestRetryAge:          row.OldestRetryAge.String(),
			OldestRetryAgeSeconds:   row.OldestRetryAge.Seconds(),
			OldestClaimAge:          row.OldestClaimAge.String(),
			OldestClaimAgeSeconds:   row.OldestClaimAge.Seconds(),
			NextRetryDelay:          row.NextRetryDelay.String(),
			NextRetryDelaySeconds:   row.NextRetryDelay.Seconds(),
			FailureClassCounts:      namedCountsJSON(row.FailureClassCounts),
		})
	}
	return projected
}
