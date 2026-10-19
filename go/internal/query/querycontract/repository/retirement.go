// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import "time"

// Bounded repository retirement labels for the read surfaces (#7766). The
// index_state is indexed | stub | retiring | retired, derived in this order:
// an open non-complete tombstone reads retiring, a complete tombstone reads
// retired, otherwise the graph evidence_source decides indexed versus stub.
const (
	// IndexStateIndexed marks a repository with canonical projector
	// evidence and no tombstone.
	IndexStateIndexed = "indexed"
	// IndexStateStub marks a repository without canonical evidence and
	// without a tombstone.
	IndexStateStub = "stub"
	// IndexStateRetiring marks a repository with an open non-complete
	// retirement tombstone: fenced, still serving frozen graph state.
	IndexStateRetiring = "retiring"
	// IndexStateRetired marks a repository whose retirement tombstone is
	// complete.
	IndexStateRetired = "retired"
)

// CanonicalEvidenceSource is the Repository evidence_source value the
// projector's canonical write path stamps. It lives beside the derivation
// so the graph page query and the label agree on the indexed anchor.
const CanonicalEvidenceSource = "projector/canonical"

// PageRetirement is the bounded tombstone label one repository list row
// carries. State and RetirementID come from the tombstone row; RetiredAt
// is set only for complete rows (HasRetiredAt reports which).
type PageRetirement struct {
	State        string
	Phase        string
	RetirementID string
	RetiredAt    time.Time
	HasRetiredAt bool
}

// IsComplete reports whether the label is retirement history rather than
// an in-flight retirement.
func (l PageRetirement) IsComplete() bool {
	return l.State == "complete"
}
