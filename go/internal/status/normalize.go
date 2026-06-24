// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import "time"

// nonNegativeDuration normalizes status-age fields that can briefly go
// negative when database timestamps are newer than the status read clock.
func nonNegativeDuration(value time.Duration) time.Duration {
	if value < 0 {
		return 0
	}
	return value
}

func normalizeQueueSnapshot(queue QueueSnapshot) QueueSnapshot {
	queue.OldestOutstandingAge = nonNegativeDuration(queue.OldestOutstandingAge)
	return queue
}

func normalizeDomainBacklogs(rows []DomainBacklog) []DomainBacklog {
	normalized := make([]DomainBacklog, 0, len(rows))
	for _, row := range rows {
		row.OldestAge = nonNegativeDuration(row.OldestAge)
		normalized = append(normalized, row)
	}
	return normalized
}
