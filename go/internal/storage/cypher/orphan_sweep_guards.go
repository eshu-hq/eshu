// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"sort"
	"strings"
)

// This file holds the orphan-sweep WHERE guards (evidence/ownership + node
// class) applied uniformly across S1, S2, and the key-anchored writes, the
// identity-key type the anti-join is computed over, and the process-local
// per-label paging cursor. Keeping them here keeps orphan_sweep.go under the
// file-length cap.

// orphanSweepKey is one node's identity inside the class its label's sweep
// owns: the property values named by orphanSweepIdentityProperties, in that
// order. Most labels have a single value; Module has two, because a canonical
// import Module is identified by (name, lang) and a Go `time` is not the
// Python `time`.
type orphanSweepKey []string

// orphanSweepKeySeparator joins key values into the single string used for map
// lookups and ordering. NUL is below every byte a property value can hold, so
// sorting the encoded form yields exactly the tuple order the emitted
// `ORDER BY key_0, key_1` produces on the backend -- which is what lets the
// paging cursor resume where the previous page stopped.
const orphanSweepKeySeparator = "\x00"

// encode renders the key for use as a Go map key and for ordering.
func (k orphanSweepKey) encode() string {
	return strings.Join(k, orphanSweepKeySeparator)
}

// encodeOrphanSweepKey is the package-level form of orphanSweepKey.encode, used
// where values arrive as a plain slice.
func encodeOrphanSweepKey(values []string) string {
	return strings.Join(values, orphanSweepKeySeparator)
}

// decodeOrphanSweepKey reverses encodeOrphanSweepKey.
func decodeOrphanSweepKey(encoded string) orphanSweepKey {
	return strings.Split(encoded, orphanSweepKeySeparator)
}

// sortOrphanSweepKeys orders keys the way the S1 read returns them, so the
// cursor taken from the last element is the true high-water mark of the page.
func sortOrphanSweepKeys(keys []orphanSweepKey) {
	sort.Slice(keys, func(i, j int) bool { return keys[i].encode() < keys[j].encode() })
}

// orphanSweepClassPredicate restricts a sweep to the single node class its
// label owns, for labels whose identity key is not unique across node classes.
// Module.name is shared: canonical imported modules are MERGEd on (name, lang)
// (no uid), while semantic module entities are MERGEd on {uid} and also carry a
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
// read. A nil cursor means start from the beginning of the label.
func (s *OrphanSweepStore) candidateCursor(label OrphanSweepLabel) orphanSweepKey {
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
// sortedKeys must be ascending (the S1 read is ORDER BY the identity key). For
// a composite-key label the cursor carries every property, so the next page
// resumes inside a name's language group rather than skipping the rest of it.
func (s *OrphanSweepStore) advanceCursor(label OrphanSweepLabel, sortedKeys []orphanSweepKey, limit int) {
	s.cursorMu.Lock()
	defer s.cursorMu.Unlock()
	if s.cursors == nil {
		s.cursors = make(map[OrphanSweepLabel]orphanSweepKey)
	}
	if limit > 0 && len(sortedKeys) >= limit {
		s.cursors[label] = sortedKeys[len(sortedKeys)-1]
		return
	}
	s.cursors[label] = nil
}
