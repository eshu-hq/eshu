// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

// This file holds the orphan-sweep WHERE guards (evidence/ownership + node
// class) applied uniformly across S1, S2, and the key-anchored writes, plus the
// process-local per-label paging cursor. Keeping them here keeps orphan_sweep.go
// under the file-length cap.

// orphanSweepClassPredicate restricts a sweep to the single node class its
// label owns, for labels whose identity key is not unique across node classes.
// Module.name is shared: canonical imported modules are MERGEd on {name} (no
// uid), while semantic module entities are MERGEd on {uid} and also carry a
// name. The orphan sweep owns only the canonical imports, so it restricts to
// `n.uid IS NULL`; without this, a connected same-name semantic module would
// mask a canonical orphan in S2 and the key-anchored writes would target the
// semantic node too. Other labels have a unique identity key and need no
// restriction.
func orphanSweepClassPredicate(label OrphanSweepLabel) string {
	if label == OrphanSweepLabelModule {
		return "n.uid IS NULL"
	}
	return ""
}

// orphanSweepNodeGuard is the full WHERE predicate identifying a node this
// label's sweep is allowed to act on: the evidence/ownership predicate plus the
// class predicate. It is applied in S1, S2, and every key-anchored write, so a
// node that changed ownership (e.g. a Repository re-created by canonical
// projection as evidence_source='projector/canonical') or is the wrong node
// class is excluded at write time, not only at the earlier read.
func orphanSweepNodeGuard(label OrphanSweepLabel) string {
	guard := orphanSweepEvidencePredicate(label)
	if class := orphanSweepClassPredicate(label); class != "" {
		guard += "\n  AND " + class
	}
	return guard
}

// candidateCursor returns the paging cursor for a label's next S1 candidate
// read (empty means start from the beginning of the label).
func (s *OrphanSweepStore) candidateCursor(label OrphanSweepLabel) string {
	s.cursorMu.Lock()
	defer s.cursorMu.Unlock()
	return s.cursors[label]
}

// advanceCursor moves a label's paging cursor past the candidate window just
// read. A full window (len == limit) resumes past the largest key seen, so the
// next cycle continues forward; a short window means the end of the label was
// reached, so the cursor wraps to "" to rescan from the start. The cursor
// advances regardless of orphan state, so a window that is entirely connected
// still makes forward progress rather than re-reading the same rows forever.
// sortedKeys must be ascending (the S1 read is ORDER BY the identity key).
func (s *OrphanSweepStore) advanceCursor(label OrphanSweepLabel, sortedKeys []string, limit int) {
	s.cursorMu.Lock()
	defer s.cursorMu.Unlock()
	if s.cursors == nil {
		s.cursors = make(map[OrphanSweepLabel]string)
	}
	if limit > 0 && len(sortedKeys) >= limit {
		s.cursors[label] = sortedKeys[len(sortedKeys)-1]
		return
	}
	s.cursors[label] = ""
}
