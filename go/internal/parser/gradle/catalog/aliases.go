// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

// aliasTable tracks the aliases of one catalog table by their normalized
// spelling (see normalizeAlias), with the exact spellings kept per slot.
//
// The same exact spelling twice is a repeated TOML key, which TOML forbids,
// so the scanner makes the file malformed, the same outcome Gradle's TOML
// reader gives.
//
// Two different spellings with one normalized form are valid TOML, and Gradle
// accepts them with a log warning: for [versions] it keeps the first
// registration and for [libraries], [plugins] and [bundles] the later entry
// replaces the earlier one. Gradle registers aliases in key-length order
// rather than file order, so the survivor is not readable from the file. The
// parser therefore never picks one: every alias in a shared slot is a
// collision, and a row or version reference that depends on it stays
// unresolved.
type aliasTable struct {
	slots   map[string]int   // normalized alias -> slot
	first   []string         // first exact spelling per slot
	later   map[int][]string // further exact spellings of a shared slot (rare)
	dropped map[int]int      // aliases past the row cap per shared slot (rare)
	kept    int              // aliases recorded with add
}

// newAliasTable returns an empty table.
func newAliasTable() *aliasTable {
	return &aliasTable{slots: map[string]int{}}
}

// full reports whether the table already keeps MaxRows aliases.
func (t *aliasTable) full() bool {
	return t.kept >= MaxRows
}

// spelled reports whether slot already holds the exact spelling alias.
func (t *aliasTable) spelled(slot int, alias string) bool {
	if t.first[slot] == alias {
		return true
	}
	for _, other := range t.later[slot] {
		if other == alias {
			return true
		}
	}
	return false
}

// add records an alias the scanner keeps. It returns the alias's slot, or
// repeated true when the exact spelling is already in the table.
func (t *aliasTable) add(alias string, buf *[]byte) (slot int, repeated bool) {
	norm := normalizeAlias(alias, buf)
	if slot, found := t.slots[string(norm)]; found {
		if t.spelled(slot, alias) {
			return slot, true
		}
		if t.later == nil {
			t.later = map[int][]string{}
		}
		t.later[slot] = append(t.later[slot], alias)
		t.kept++
		return slot, false
	}
	slot = len(t.first)
	key := alias // most aliases need no normalization: reuse the string
	if string(norm) != alias {
		key = string(norm)
	}
	t.slots[key] = slot
	t.first = append(t.first, alias)
	t.kept++
	return slot, false
}

// addDropped records an alias past the row cap. It only marks a collision
// with an alias the table keeps, since Gradle would still register the
// dropped alias and may replace that alias's entry; an alias that collides
// with nothing costs a lookup and no allocation. repeated is true when the
// exact spelling is already kept.
func (t *aliasTable) addDropped(alias string, buf *[]byte) (repeated bool) {
	slot, found := t.slots[string(normalizeAlias(alias, buf))]
	if !found {
		return false
	}
	if t.spelled(slot, alias) {
		return true
	}
	if t.dropped == nil {
		t.dropped = map[int]int{}
	}
	t.dropped[slot]++
	return false
}

// members counts the aliases, kept or dropped, that normalize to slot.
func (t *aliasTable) members(slot int) int {
	return 1 + len(t.later[slot]) + t.dropped[slot]
}

// lookup returns the slot of a normalized spelling and whether it is shared.
func (t *aliasTable) lookup(name string, buf *[]byte) (slot int, found, shared bool) {
	slot, found = t.slots[string(normalizeAlias(name, buf))]
	if !found {
		return 0, false, false
	}
	return slot, true, t.shared(slot)
}

// shared reports whether more than one alias normalizes to slot.
func (t *aliasTable) shared(slot int) bool {
	return t.members(slot) > 1
}

// collided counts the aliases that share a slot with another alias. Only
// shared slots appear in later or dropped, so the walk is over collisions.
func (t *aliasTable) collided() int {
	total := 0
	for slot := range t.later {
		total += t.members(slot)
	}
	for slot := range t.dropped {
		if _, counted := t.later[slot]; !counted {
			total += t.members(slot)
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
