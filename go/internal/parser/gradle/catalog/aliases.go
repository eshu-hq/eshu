// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

// aliasTable tracks the aliases of one catalog table under two spellings.
//
// exact holds each alias as the TOML key spells it. TOML forbids a repeated
// key, so an exact repeat makes the file malformed, the same outcome Gradle's
// TOML reader gives.
//
// slots holds each alias after Gradle's normalization (see normalizeAlias).
// Two aliases with one normalized spelling are valid TOML, and Gradle accepts
// them with a log warning: for [versions] it keeps the first registration and
// for [libraries], [plugins] and [bundles] the later entry replaces the
// earlier one. Gradle registers aliases in key-length order rather than file
// order, so the survivor is not readable from the file. The parser therefore
// never picks one: every alias in a shared slot is a collision, and a row or
// version reference that depends on it stays unresolved.
type aliasTable struct {
	exact   map[string]struct{}
	slots   map[string]int // normalized alias -> index into members
	members []int          // aliases (emitted or dropped) per slot
}

// newAliasTable returns an empty table.
func newAliasTable() *aliasTable {
	return &aliasTable{exact: map[string]struct{}{}, slots: map[string]int{}}
}

// repeated reports whether alias was already seen with the exact spelling.
func (t *aliasTable) repeated(alias string) bool {
	_, found := t.exact[alias]
	return found
}

// full reports whether the table already tracks MaxRows aliases.
func (t *aliasTable) full() bool {
	return len(t.exact) >= MaxRows
}

// add records an alias the scanner keeps and returns its slot.
func (t *aliasTable) add(alias string, buf *[]byte) int {
	t.exact[alias] = struct{}{}
	norm := normalizeAlias(alias, buf)
	if slot, found := t.slots[string(norm)]; found {
		t.members[slot]++
		return slot
	}
	slot := len(t.members)
	t.slots[string(norm)] = slot
	t.members = append(t.members, 1)
	return slot
}

// addDropped records an alias past the row cap. It allocates nothing: the
// alias only marks a collision with an alias the table already holds, since
// Gradle would still register it and may replace that alias's entry.
func (t *aliasTable) addDropped(alias string, buf *[]byte) {
	if slot, found := t.slots[string(normalizeAlias(alias, buf))]; found {
		t.members[slot]++
	}
}

// lookup returns the slot of a normalized spelling and whether it is shared.
func (t *aliasTable) lookup(name string, buf *[]byte) (slot int, found, shared bool) {
	slot, found = t.slots[string(normalizeAlias(name, buf))]
	if !found {
		return 0, false, false
	}
	return slot, true, t.members[slot] > 1
}

// shared reports whether more than one alias normalizes to slot.
func (t *aliasTable) shared(slot int) bool {
	return t.members[slot] > 1
}

// collided counts the aliases that share a slot with another alias.
func (t *aliasTable) collided() int {
	total := 0
	for _, n := range t.members {
		if n > 1 {
			total += n
		}
	}
	return total
}

// normalizeAlias writes alias into buf with '-' and '_' replaced by '.', the
// rule of Gradle's AliasNormalizer (it maps each of '-', '_' and '.' to '.'),
// and returns the result. buf is reused, so the result is valid only until
// the next call; map lookups through string(result) do not allocate.
func normalizeAlias(alias string, buf *[]byte) []byte {
	out := (*buf)[:0]
	for i := 0; i < len(alias); i++ {
		b := alias[i]
		if b == '-' || b == '_' {
			b = '.'
		}
		out = append(out, b)
	}
	*buf = out
	return out
}
