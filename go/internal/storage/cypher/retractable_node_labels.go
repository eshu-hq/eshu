// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import "sort"

// fileAndDirectoryRetractLabels are the structural graph node labels the retract
// phase tombstones outside the per-domain entity-label sets: File (via
// canonicalNodeRetractFilesCypher / canonicalNodeRetractRemovedFilesCypher and the
// delta canonicalNodeRetractDeltaDeletedFilesCypher) and Directory (via the delta
// canonicalNodeRetractDeltaEmptyDirectoriesCypher — the #4186 directory-tombstone
// class itself). They are retracted by dedicated statements in
// buildRetractStatements/buildDeltaRetractStatements rather than the entity-label
// scan, so they must be added explicitly or the delta denominator would omit the
// exact node types #4186 is about.
var fileAndDirectoryRetractLabels = []string{"Directory", "File"}

// terraformStateRetractLabels are the tfstate canonical writer's own
// dedicated-statement retract labels (#5443:
// terraformStateResourceRetractStatements in
// tfstate_canonical_writer_retract.go), outside the per-domain entity-label
// sets for the same reason fileAndDirectoryRetractLabels is: they are
// retracted by standalone generation-gated DETACH DELETE statements scoped
// by scope_id, not the repo_id-scoped generic entity-label scan.
var terraformStateRetractLabels = []string{"TerraformStateResource"}

// RetractableNodeEntityLabels returns the sorted, de-duplicated set of graph node
// labels the canonical retract phase can tombstone: the per-domain entity-label
// sets the retract phase scans, plus the structural File and Directory labels the
// dedicated file/directory retract statements remove, plus the tfstate writer's
// own dedicated-statement labels.
//
// It is the lockstep source of truth for replay depth coverage (epic #4172,
// C-13 issue #4366): the replay-coverage gate requires a delta/tombstone replay
// scenario for every retractable node type, and a lockstep test keeps
// specs/replay-depth-requirements.v1.yaml byte-equal to this set. Adding a label
// to a retract set therefore makes the gate demand a new delta scenario for it
// (the #4186 directory-tombstone class), instead of the gap going unseen.
func RetractableNodeEntityLabels() []string {
	labels := canonicalNodeRetractEntityLabels()
	labels = append(labels, fileAndDirectoryRetractLabels...)
	labels = append(labels, terraformStateRetractLabels...)
	sort.Strings(labels)
	return labels
}
