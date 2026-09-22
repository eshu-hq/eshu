// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"github.com/eshu-hq/eshu/go/internal/status/shared"
)

func normalizeQueueSnapshot(snapshot QueueSnapshot) QueueSnapshot {
	snapshot.OldestOutstandingAge = shared.NonNegativeDuration(snapshot.OldestOutstandingAge)
	return snapshot
}

func normalizeDomainBacklogs(rows []DomainBacklog) []DomainBacklog {
	normalized := make([]DomainBacklog, 0, len(rows))
	for _, row := range rows {
		row.OldestAge = shared.NonNegativeDuration(row.OldestAge)
		normalized = append(normalized, row)
	}
	return normalized
}
