// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import "sort"

// Snapshot is the durable, serializable form of a Store. It carries each known
// function's identity, effects, and resolved content version so the Store can be
// reloaded across runs without recomputation. It is a plain value with exported
// fields, so encoding/json (or any codec) can persist it.
type Snapshot struct {
	Functions []SnapshotFunction `json:"functions"`
}

// SnapshotFunction is one function's durable summary.
type SnapshotFunction struct {
	ID      FunctionID `json:"id"`
	Effects Effects    `json:"effects"`
	Version string     `json:"version"`
}

// Snapshot returns the durable form of the Store, ordered by function ID for a
// byte-stable encoding.
func (s *Store) Snapshot() Snapshot {
	snap := Snapshot{}
	for _, id := range s.IDs() {
		e := s.entries[id]
		// A known function is always versioned after Upsert; skip any that is
		// not rather than persist an empty version that would reload as a
		// permanently unversioned entry.
		if !e.hasVersion {
			continue
		}
		snap.Functions = append(snap.Functions, SnapshotFunction{
			ID:      id,
			Effects: e.effects,
			Version: e.version,
		})
	}
	return snap
}

// Load rebuilds a Store from a Snapshot, restoring effects, reverse edges, and
// versions exactly as persisted. It does not recompute versions: a subsequent
// Upsert with unchanged effects recomputes nothing, which is what proves the
// reloaded state is consistent.
func Load(snap Snapshot) *Store {
	s := NewStore()
	// First install all effects and reverse edges.
	functions := append([]SnapshotFunction(nil), snap.Functions...)
	sort.Slice(functions, func(i, j int) bool { return functions[i].ID < functions[j].ID })
	for _, fn := range functions {
		e := s.entries[fn.ID]
		if e == nil {
			e = &entry{callers: map[FunctionID]struct{}{}}
			s.entries[fn.ID] = e
		}
		e.effects = fn.Effects
		e.callees = fn.Effects.callees()
		e.structHash = structuralHash(fn.Effects)
		e.version = fn.Version
		e.hasVersion = fn.Version != ""
		e.known = true
		for _, callee := range e.callees {
			ce := s.entries[callee]
			if ce == nil {
				ce = &entry{callers: map[FunctionID]struct{}{}}
				s.entries[callee] = ce
			}
			ce.callers[fn.ID] = struct{}{}
		}
	}
	return s
}
