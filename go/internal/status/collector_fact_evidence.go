// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"slices"
	"time"
)

// CollectorFactEvidence summarizes persisted source or reducer fact evidence
// for one collector runtime without exposing source payload identifiers.
type CollectorFactEvidence struct {
	InstanceID       string
	CollectorKind    string
	EvidenceSource   string
	SourceSystems    []string
	ObservationCount int
	LastObservedAt   time.Time
	UpdatedAt        time.Time
}

func cloneCollectorFactEvidence(rows []CollectorFactEvidence) []CollectorFactEvidence {
	cloned := slices.Clone(rows)
	for i := range cloned {
		cloned[i].SourceSystems = slices.Clone(rows[i].SourceSystems)
	}
	return cloned
}
