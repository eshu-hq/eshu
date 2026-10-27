// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nesting

import (
	"fmt"

	"github.com/pelletier/go-toml/v2/unstable"
)

// This file holds a step-counting copy of go-toml v2.3.1
// internal/tracker/seen.go (MIT, Copyright (c) 2021 - 2022 Thomas Pelletier
// and Eric Anderton). The upstream package is internal, so the oracle cannot
// import it. The copy keeps every scan the original does and adds one counter
// tick per list entry visited, which is the unit of work the key-work bound
// estimates. When go-toml is upgraded, diff this copy against the new
// seen.go.

type stepKind uint8

const (
	stepInvalid stepKind = iota
	stepValue
	stepTable
	stepArrayTable
)

type stepEntry struct {
	child, next int
	name        []byte
	kind        stepKind
	explicit    bool
	kv          bool
}

// stepTracker mirrors tracker.SeenTracker. steps is shared with the trackers
// of inline tables, so one document has one total.
type stepTracker struct {
	entries    []stepEntry
	currentIdx int
	steps      *int
}

func newStepTracker(steps *int) *stepTracker {
	t := &stepTracker{steps: steps}
	t.reset()
	return t
}

func (s *stepTracker) reset() {
	s.currentIdx = 0
	s.entries = make([]stepEntry, 1, 2)
	s.entries[0].child = -1
	s.entries[0].next = -1
}

func (s *stepTracker) find(parentIdx int, k []byte) int {
	for i := s.entries[parentIdx].child; i >= 0; i = s.entries[i].next {
		*s.steps++
		if string(s.entries[i].name) == string(k) {
			return i
		}
	}
	return -1
}

func (s *stepTracker) clear(idx int) {
	if idx >= len(s.entries) {
		return
	}
	for i := s.entries[idx].child; i >= 0; {
		*s.steps++
		next := s.entries[i].next
		n := s.entries[0].next
		s.entries[0].next = i
		s.entries[i].next = n
		s.entries[i].name = nil
		s.clear(i)
		i = next
	}
	s.entries[idx].child = -1
}

func (s *stepTracker) create(parentIdx int, name []byte, kind stepKind, explicit, kv bool) int {
	e := stepEntry{child: -1, next: s.entries[parentIdx].child, name: name, kind: kind, explicit: explicit, kv: kv}
	var idx int
	if s.entries[0].next >= 0 {
		idx = s.entries[0].next
		s.entries[0].next = s.entries[idx].next
		s.entries[idx] = e
	} else {
		idx = len(s.entries)
		s.entries = append(s.entries, e)
	}
	s.entries[parentIdx].child = idx
	return idx
}

func (s *stepTracker) setExplicitFlag(parentIdx int) {
	for i := s.entries[parentIdx].child; i >= 0; i = s.entries[i].next {
		*s.steps++
		if s.entries[i].kv {
			s.entries[i].explicit = true
			s.entries[i].kv = false
		}
		s.setExplicitFlag(i)
	}
}

// check is CheckExpression.
func (s *stepTracker) check(node *unstable.Node) (bool, error) {
	switch node.Kind {
	case unstable.KeyValue:
		return s.checkKeyValue(node)
	case unstable.Table:
		return s.checkTable(node)
	case unstable.ArrayTable:
		return s.checkArrayTable(node)
	default:
		return false, fmt.Errorf("unexpected top level node %s", node.Kind)
	}
}

func (s *stepTracker) checkTable(node *unstable.Node) (bool, error) {
	if s.currentIdx >= 0 {
		s.setExplicitFlag(s.currentIdx)
	}
	it := node.Key()
	parentIdx := 0
	for it.Next() {
		if it.IsLast() {
			break
		}
		k := it.Node().Data
		idx := s.find(parentIdx, k)
		if idx < 0 {
			idx = s.create(parentIdx, k, stepTable, false, false)
		} else if s.entries[idx].kind == stepValue {
			return false, fmt.Errorf("expected %s to be a table", k)
		}
		parentIdx = idx
	}
	k := it.Node().Data
	idx := s.find(parentIdx, k)
	first := false
	if idx >= 0 {
		if s.entries[idx].kind != stepTable {
			return false, fmt.Errorf("key %s should be a table", k)
		}
		if s.entries[idx].explicit {
			return false, fmt.Errorf("table %s already exists", k)
		}
		s.entries[idx].explicit = true
	} else {
		idx = s.create(parentIdx, k, stepTable, true, false)
		first = true
	}
	s.currentIdx = idx
	return first, nil
}

func (s *stepTracker) checkArrayTable(node *unstable.Node) (bool, error) {
	if s.currentIdx >= 0 {
		s.setExplicitFlag(s.currentIdx)
	}
	it := node.Key()
	parentIdx := 0
	for it.Next() {
		if it.IsLast() {
			break
		}
		k := it.Node().Data
		idx := s.find(parentIdx, k)
		if idx < 0 {
			idx = s.create(parentIdx, k, stepTable, false, false)
		} else if s.entries[idx].kind == stepValue {
			return false, fmt.Errorf("expected %s to be a table", k)
		}
		parentIdx = idx
	}
	k := it.Node().Data
	idx := s.find(parentIdx, k)
	firstTime := idx < 0
	if firstTime {
		idx = s.create(parentIdx, k, stepArrayTable, true, false)
	} else {
		if s.entries[idx].kind != stepArrayTable {
			return false, fmt.Errorf("key %s already exists as a non array table", k)
		}
		s.clear(idx)
	}
	s.currentIdx = idx
	return firstTime, nil
}

func (s *stepTracker) checkKeyValue(node *unstable.Node) (bool, error) {
	parentIdx := s.currentIdx
	it := node.Key()
	for it.Next() {
		k := it.Node().Data
		idx := s.find(parentIdx, k)
		if idx < 0 {
			idx = s.create(parentIdx, k, stepTable, false, true)
		} else {
			entry := s.entries[idx]
			switch {
			case it.IsLast():
				return false, fmt.Errorf("key %s is already defined", k)
			case entry.kind != stepTable:
				return false, fmt.Errorf("expected %s to be a table", k)
			case entry.explicit:
				return false, fmt.Errorf("cannot redefine table %s", k)
			}
		}
		parentIdx = idx
	}
	s.entries[parentIdx].kind = stepValue
	value := node.Value()
	switch value.Kind {
	case unstable.InlineTable:
		return s.checkInlineTable(value)
	case unstable.Array:
		return s.checkArray(value)
	default:
		return false, nil
	}
}

func (s *stepTracker) checkArray(node *unstable.Node) (bool, error) {
	it := node.Children()
	first := false
	for it.Next() {
		n := it.Node()
		var err error
		switch n.Kind { //nolint:exhaustive
		case unstable.InlineTable:
			first, err = s.checkInlineTable(n)
		case unstable.Array:
			first, err = s.checkArray(n)
		}
		if err != nil {
			return false, err
		}
	}
	return first, nil
}

func (s *stepTracker) checkInlineTable(node *unstable.Node) (bool, error) {
	inner := newStepTracker(s.steps)
	first := false
	it := node.Children()
	for it.Next() {
		var err error
		first, err = inner.checkKeyValue(it.Node())
		if err != nil {
			return false, err
		}
	}
	return first, nil
}

// decodeShape says which top-level tables a struct target knows, because
// that is all that decides which expressions go-toml tracker-checks. A nil
// decodeShape is a map decode: the tracker sees every expression.
type decodeShape struct {
	// knownTables holds the single-segment table names that match a field
	// of the target's root struct.
	knownTables map[string]bool
}

// trackerSteps parses doc with go-toml's own parser and replays every
// expression through the step-counting tracker the way the decoder does. It
// returns the total steps, or an error for a document go-toml rejects.
//
// The replay mirrors go-toml v2.3.1 unmarshaler.go: handleRootExpression
// skips CheckExpression for a key/value only while skipUntilTable is set,
// handleKeyValues (the loop inside a table the target knows) calls
// CheckExpression unconditionally, and handleKeyValue clears
// skipUntilTable at the end of the same key that set it. So an unknown key
// never suppresses the tracker: skip lasts only from a table header the
// target does not know (handleKeyPart sets skipUntilTable and the table's
// entries are never entered) until the next header. Headers themselves are
// always checked.
func trackerSteps(doc []byte, shape *decodeShape) (int, error) {
	var steps int
	tracker := newStepTracker(&steps)
	var p unstable.Parser
	p.Reset(doc)
	skip := false
	for p.NextExpression() {
		expr := p.Expression()
		switch expr.Kind {
		case unstable.KeyValue:
			if !skip {
				if _, err := tracker.check(expr); err != nil {
					return steps, err
				}
			}
		case unstable.Table, unstable.ArrayTable:
			if _, err := tracker.check(expr); err != nil {
				return steps, err
			}
			skip = shape != nil && !shape.knowsHeader(expr)
		default:
			if _, err := tracker.check(expr); err != nil {
				return steps, err
			}
		}
	}
	return steps, p.Error()
}

// knowsHeader reports whether a table header names a root field of the
// target struct. Only a single-segment header can: a dotted header whose
// first segment is unknown is skipped at its first segment, and one under a
// known table names a nested field the replay does not model, so it counts
// as unknown and its entries go unchecked here. The guard charges dotted
// headers' entries in full, so this can only understate the steps, never
// overstate them.
func (s *decodeShape) knowsHeader(expr *unstable.Node) bool {
	return segmentCount(expr) == 1 && knownTable(s.knownTables, firstSegment(expr))
}

func firstSegment(expr *unstable.Node) string {
	it := expr.Key()
	it.Next()
	return string(it.Node().Data)
}

func segmentCount(expr *unstable.Node) int {
	n := 0
	it := expr.Key()
	for it.Next() {
		n++
	}
	return n
}
