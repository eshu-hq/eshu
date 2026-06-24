// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import "sort"

// Store holds function summaries and recomposes their content versions
// incrementally. It is not safe for concurrent use; a caller serializes access
// or shards by conflict key.
type Store struct {
	entries map[FunctionID]*entry
}

type entry struct {
	effects    Effects
	callees    []FunctionID
	structHash string
	version    string
	hasVersion bool
	// known is false for a placeholder created because another function referred
	// to it before its own effects were upserted. A placeholder holds reverse
	// edges but is never recomputed and contributes an empty external version.
	known   bool
	callers map[FunctionID]struct{}
}

// NewStore returns an empty Store.
func NewStore() *Store {
	return &Store{entries: map[FunctionID]*entry{}}
}

// Version returns the content version of a function, if known.
func (s *Store) Version(id FunctionID) (string, bool) {
	e, ok := s.entries[id]
	if !ok || !e.hasVersion {
		return "", false
	}
	return e.version, true
}

// Summary returns the resolved summary of a function, if known.
func (s *Store) Summary(id FunctionID) (Summary, bool) {
	e, ok := s.entries[id]
	if !ok || !e.hasVersion {
		return Summary{}, false
	}
	return Summary{ID: id, Effects: e.effects, Callees: e.callees, Version: e.version}, true
}

// IDs returns all known function IDs, sorted. Placeholders are excluded.
func (s *Store) IDs() []FunctionID {
	out := make([]FunctionID, 0, len(s.entries))
	for id, e := range s.entries {
		if e.known {
			out = append(out, id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Upsert installs or updates the effects of the given functions and recomposes
// only the affected content versions: a function is recomputed when its own
// structural facts changed or when a callee outside its strongly-connected
// component changed version. It returns the sorted IDs whose version was
// recomputed.
func (s *Store) Upsert(updates map[FunctionID]Effects) []FunctionID {
	dirty := map[FunctionID]struct{}{}
	for _, fnID := range sortedKeys(updates) {
		if s.applyUpdate(fnID, updates[fnID]) {
			dirty[fnID] = struct{}{}
		}
	}
	if len(dirty) == 0 {
		return nil
	}

	order, sccID := s.condense()
	recomputed := s.recompute(order, sccID, dirty)
	sort.Slice(recomputed, func(i, j int) bool { return recomputed[i] < recomputed[j] })
	return recomputed
}

// applyUpdate installs a function's effects and maintains reverse edges. It
// returns true when the function is new or its structural facts or callee set
// changed (so it must be recomputed).
func (s *Store) applyUpdate(fnID FunctionID, eff Effects) bool {
	newHash := structuralHash(eff)
	newCallees := eff.callees()

	e := s.entries[fnID]
	if e == nil {
		e = &entry{callers: map[FunctionID]struct{}{}}
		s.entries[fnID] = e
	}
	changed := !e.known || e.structHash != newHash || !sameCallees(e.callees, newCallees)

	// Rewire reverse edges: drop this function from old callees, add to new.
	for _, old := range e.callees {
		if callee := s.entries[old]; callee != nil {
			delete(callee.callers, fnID)
		}
	}
	e.effects = eff
	e.callees = newCallees
	e.structHash = newHash
	e.known = true
	for _, callee := range newCallees {
		ce := s.entries[callee]
		if ce == nil {
			ce = &entry{callers: map[FunctionID]struct{}{}}
			s.entries[callee] = ce
		}
		ce.callers[fnID] = struct{}{}
	}
	return changed
}

// recompute walks functions in reverse-topological order (callees before
// callers) and recomputes the version of every dirty function, propagating a
// changed version to its callers. Versions exclude same-SCC callee versions, so
// the pass terminates on cycles.
func (s *Store) recompute(order []FunctionID, sccID map[FunctionID]int, dirty map[FunctionID]struct{}) []FunctionID {
	var recomputed []FunctionID
	for _, fnID := range order {
		if _, isDirty := dirty[fnID]; !isDirty {
			continue
		}
		e := s.entries[fnID]
		if e == nil || !e.known {
			continue
		}
		newVersion := contentVersion(e.structHash, s.externalCalleeVersions(fnID, sccID))
		if e.hasVersion && newVersion == e.version {
			continue
		}
		e.version = newVersion
		e.hasVersion = true
		recomputed = append(recomputed, fnID)
		for caller := range e.callers {
			dirty[caller] = struct{}{}
		}
	}
	return recomputed
}

// externalCalleeVersions returns the versions of a function's callees that lie
// outside its strongly-connected component, in callee-ID order (contentVersion
// re-sorts for hash stability).
func (s *Store) externalCalleeVersions(fnID FunctionID, sccID map[FunctionID]int) []string {
	e := s.entries[fnID]
	var versions []string
	for _, callee := range e.callees {
		if sccID[callee] == sccID[fnID] {
			continue
		}
		if ce := s.entries[callee]; ce != nil && ce.hasVersion {
			versions = append(versions, ce.version)
		}
	}
	return versions
}

// sameCallees reports whether two sorted callee lists are identical.
func sameCallees(a, b []FunctionID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// sortedKeys returns the update keys in sorted order for deterministic
// processing.
func sortedKeys(updates map[FunctionID]Effects) []FunctionID {
	out := make([]FunctionID, 0, len(updates))
	for id := range updates {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
