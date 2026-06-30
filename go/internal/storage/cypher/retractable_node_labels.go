// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

// RetractableNodeEntityLabels returns the sorted, de-duplicated set of graph
// node entity labels the canonical retract phase can tombstone (the union of the
// per-domain retract label sets the retract phase scans).
//
// It is the lockstep source of truth for replay depth coverage (epic #4172,
// C-13 issue #4366): the replay-coverage gate requires a delta/tombstone replay
// scenario for every retractable node type, and a lockstep test keeps
// specs/replay-depth-requirements.v1.yaml byte-equal to this set. Adding a label
// to a retract set therefore makes the gate demand a new delta scenario for it
// (the #4186 directory-tombstone class), instead of the gap going unseen.
func RetractableNodeEntityLabels() []string {
	return canonicalNodeRetractEntityLabels()
}
