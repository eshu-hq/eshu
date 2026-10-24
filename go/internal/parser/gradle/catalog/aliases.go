// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

// aliasTable tracks the aliases of one catalog table by their accessor key
// (see accessorKey), with the exact spellings kept per slot.
//
// The same exact spelling twice is a repeated TOML key, which TOML forbids,
// so the scanner makes the file malformed, the same outcome Gradle's TOML
// reader gives.
//
// Two different spellings with one normalized form (see sameNormalized) are
// valid TOML, and Gradle accepts them with a log warning: for [versions] it
// keeps the first registration and for [libraries], [plugins] and [bundles]
// the later entry replaces the earlier one. Gradle registers aliases in
// key-length order rather than file order, so the survivor is not readable
// from the file. Two aliases with different normalized forms but one
// accessor name (fooBar and foo-bar) fail Gradle's accessor generation. The
// parser therefore never picks one: every alias in a shared slot is a
// collision, and a row or version reference that depends on it stays
// unresolved. One normalized form has one accessor key, so keying slots on
// the accessor key covers both cases.
//
// Every check is one map operation, so a file of many spellings of one alias
// (3^k separator variants) stays linear.
type aliasTable struct {
	slots map[string]int      // accessor key -> slot
	first []string            // first exact spelling per slot
	later map[string]struct{} // exact spellings after the first (rare)
	extra map[int]int         // later and dropped aliases per shared slot (rare)
	kept  int                 // aliases recorded with add
}

// newAliasTable returns an empty table.
func newAliasTable() *aliasTable {
	return &aliasTable{slots: map[string]int{}}
}

// full reports whether the table already keeps MaxRows aliases.
func (t *aliasTable) full() bool {
	return t.kept >= MaxRows
}

// spelled reports whether slot already holds the exact spelling alias. An
// exact spelling has one accessor key, so a hit in later is in slot.
func (t *aliasTable) spelled(slot int, alias string) bool {
	if t.first[slot] == alias {
		return true
	}
	_, found := t.later[alias]
	return found
}

// share counts one more alias, kept or dropped, in slot.
func (t *aliasTable) share(slot int) {
	if t.extra == nil {
		t.extra = map[int]int{}
	}
	t.extra[slot]++
}

// add records an alias the scanner keeps. It returns the alias's slot, or
// repeated true when the exact spelling is already in the table.
func (t *aliasTable) add(alias string, buf *[]byte) (slot int, repeated bool) {
	key := accessorKey(alias, buf)
	if slot, found := t.slots[string(key)]; found {
		if t.spelled(slot, alias) {
			return slot, true
		}
		if t.later == nil {
			t.later = map[string]struct{}{}
		}
		t.later[alias] = struct{}{}
		t.share(slot)
		t.kept++
		return slot, false
	}
	return t.open(alias, key), false
}

// open gives alias a new slot under key. Most aliases are their own accessor
// key (no separator, lower-case first letter), so the alias string is reused.
func (t *aliasTable) open(alias string, key []byte) int {
	slot := len(t.first)
	stored := alias
	if string(key) != alias {
		stored = string(key)
	}
	t.slots[stored] = slot
	t.first = append(t.first, alias)
	t.kept++
	return slot
}

// shadow records an alias Gradle registers but the grammar emits no row for:
// the first segment of a dotted key (alias.module = ...), the alias of a
// [libraries.alias] sub-table, or an alias longer than MaxFieldBytes. It
// collides with other spellings like any alias. Repeating the same exact
// spelling is one alias (alias.module and alias.version), so it is a no-op;
// a real repeat (alias.module twice, or alias = ... with alias.module) is
// invalid TOML that keyTree rejects. Past the row cap it only marks a collision with a kept alias, as
// addDropped does. alias is copied only when it takes a new slot or spelling;
// the conversions in the comparisons and lookups below do not allocate.
func (t *aliasTable) shadow(alias []byte, buf *[]byte) {
	key := accessorKey(alias, buf)
	slot, found := t.slots[string(key)]
	if found && t.first[slot] == string(alias) {
		return
	}
	if found {
		if _, later := t.later[string(alias)]; later {
			return
		}
	}
	switch {
	case found:
		// A new spelling of a kept alias. It is recorded even past the cap,
		// so its own repeats (alias.module, alias.version) count once.
		if t.later == nil {
			t.later = map[string]struct{}{}
		}
		t.later[string(alias)] = struct{}{}
		t.share(slot)
		if !t.full() {
			t.kept++
		}
	case !t.full():
		t.open(string(alias), key)
	default:
		// Past the cap an alias that collides with nothing is not tracked.
	}
}

// addDropped records an alias past the row cap. It only marks a collision
// with an alias the table keeps, since Gradle would still register the
// dropped alias and may replace that alias's entry; an alias that collides
// with nothing costs a lookup and no allocation. repeated is true when the
// exact spelling is already kept.
func (t *aliasTable) addDropped(alias string, buf *[]byte) (repeated bool) {
	slot, found := t.slots[string(accessorKey(alias, buf))]
	if !found {
		return false
	}
	if t.spelled(slot, alias) {
		return true
	}
	t.share(slot)
	return false
}

// members counts the aliases, kept or dropped, that share slot's accessor key.
func (t *aliasTable) members(slot int) int {
	return 1 + t.extra[slot]
}

// lookup returns the slot whose alias normalizes like name, and whether the
// slot is shared. Gradle looks a reference up by normalized alias, not by
// accessor name, so an unshared slot is found only when its one alias
// normalizes to name's form; a shared slot never resolves, so it is reported
// shared without that check.
func (t *aliasTable) lookup(name string, buf *[]byte) (slot int, found, shared bool) {
	slot, found = t.slots[string(accessorKey(name, buf))]
	if !found {
		return 0, false, false
	}
	if t.shared(slot) {
		return slot, true, true
	}
	if !sameNormalized(t.first[slot], name) {
		return 0, false, false
	}
	return slot, true, false
}

// shared reports whether more than one alias has slot's accessor key.
func (t *aliasTable) shared(slot int) bool {
	return t.members(slot) > 1
}

// collided counts the aliases that share a slot with another alias. Only
// shared slots appear in extra, so the walk is over collisions.
func (t *aliasTable) collided() int {
	total := 0
	for slot := range t.extra {
		total += t.members(slot)
	}
	return total
}

// isSeparator reports whether b is one of the alias separators '-', '_' and
// '.', which Gradle's AliasNormalizer maps to '.' and its toJavaName splits on.
func isSeparator(b byte) bool {
	return b == '-' || b == '_' || b == '.'
}

// sameNormalized reports whether a and b have one normalized form under
// Gradle's AliasNormalizer: equal except that any separator matches any
// separator. It allocates nothing.
func sameNormalized(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if a[i] != b[i] && (!isSeparator(a[i]) || !isSeparator(b[i])) {
			return false
		}
	}
	return true
}

// accessorKey writes into buf the accessor name Gradle's toJavaName derives
// from alias, with its first letter lower-cased, and returns it. toJavaName
// splits on '.', '-' and '_' and capitalizes each part, so fooBar, foo-bar
// and foo.bar share the key fooBar. Lower-casing the first letter keeps the
// equivalence (Gradle's alias pattern starts with [a-z]) and lets most
// aliases be their own key. Only ASCII letters change case: Gradle rejects a
// non-ASCII alias. buf is reused, so the result is valid only until the next
// call; map lookups through string(result) do not allocate. alias is a
// string or the key bytes of a parser node, so neither form is copied.
func accessorKey[T string | []byte](alias T, buf *[]byte) []byte {
	out := (*buf)[:0]
	upper := false
	for i := 0; i < len(alias); i++ {
		b := alias[i]
		switch {
		case isSeparator(b):
			upper = true
			continue
		case len(out) == 0 && 'A' <= b && b <= 'Z':
			b += 'a' - 'A'
		case len(out) > 0 && upper && 'a' <= b && b <= 'z':
			b -= 'a' - 'A'
		}
		upper = false
		out = append(out, b)
	}
	*buf = out
	return out
}
