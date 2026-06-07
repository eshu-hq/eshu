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
	ObservationCount int
	LastObservedAt   time.Time
	UpdatedAt        time.Time
}

func cloneCollectorFactEvidence(rows []CollectorFactEvidence) []CollectorFactEvidence {
	return slices.Clone(rows)
}
