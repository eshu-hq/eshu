// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nesting

import (
	"errors"
	"fmt"
	"hash/maphash"
	"strings"
)

// ErrTooManyKeys reports a document whose tables hold so many keys that a
// decoder's key lookup would run for far longer than any real file needs.
// go-toml v2.3.1 finds a key by scanning a linked list, so decoding a table
// of n keys costs n squared steps: 100,000 keys under one [dependencies]
// header hold a parse worker for about 18 seconds. Callers treat the file as
// unparseable, the same as ErrTooDeep.
var ErrTooManyKeys = errors.New("toml key-table work exceeds limit")

// Bounds are the limits CheckBounds enforces. A zero field allows nothing, so
// a caller that leaves one out fails closed, except KnownTables where empty
// is the strictest reading (every entry is charged).
type Bounds struct {
	// Depth is the deepest nesting allowed, as for Check.
	Depth int
	// KeyWork bounds the decoder's key-lookup steps. Each lookup is charged
	// the length of the sibling list it scans, so the total tracks the steps
	// go-toml takes. See CheckBounds.
	KeyWork int
	// KnownTables names the top-level tables a struct decode target knows:
	// its root fields' TOML names ({"package": true} for a Cargo.lock),
	// matched case-insensitively the way go-toml matches struct fields.
	// go-toml tracker-checks every root key and every key in a table the
	// target knows, known field or not, but never enters a table the target
	// does not know, so plain entries under a single-segment header that
	// names no known table are not charged. Headers, inline tables, dotted
	// headers, and quoted headers are charged in full either way. A nil or
	// empty set charges every entry: the right reading for a map target, and
	// the strictest for any target. A caller that sets it must name every
	// table its target knows, because a table left out would be skipped by
	// the guard but tracked by the decoder.
	KnownTables map[string]bool
}

// keyWork is the running lookup-step estimate of one pass.
//
// go-toml v2.3.1 keeps the keys it has seen in a tree whose children form a
// singly linked list, and every key or header segment it defines or checks
// scans its parent's list. A table that holds n entries therefore costs n
// squared steps, and a header that names a parent with n children costs n
// steps every time it appears, a repeated [[package]] section included.
// The estimate charges each lookup an upper bound of the list it scans:
//
//   - a plain entry with m key segments costs m times the entries its scope
//     holds so far plus the distinct headers so far, since a table may also
//     hold children made by earlier headers;
//   - an inline-table entry costs m times the entries of that inline table,
//     which has a tracker of its own;
//   - a header with s segments costs s+1 times the distinct headers so far
//     plus the most entries any scope has held, which covers its segment
//     lookups and the walk over the table it replaces.
//
// The one exemption is a scope under a single-segment header that names no
// table in Bounds.KnownTables: go-toml never enters such a table for a
// struct target, so its plain entries add no tracker entries and cost
// nothing. Every other key is charged, in known tables too: the decoder
// tracker-checks unknown keys there and clears its skip at the end of each
// key, so no per-key or field-count skip exists to mirror.
type keyWork struct {
	total int
	limit int
	// knownTables is Bounds.KnownTables.
	knownTables map[string]bool
	// scopeEntries counts the key segments the plain entries of the table
	// under the latest header defined.
	scopeEntries int
	// scopeSkipped is true while the latest header names a table the target
	// does not know, so plain entries are free until the next header.
	scopeSkipped bool
	// maxEntries is the largest scopeEntries any scope has reached.
	maxEntries int
	headers    headerSet
}

// startScope begins the table under a new header. The scope counts as
// tracked until the header closes and header proves it unknown.
func (w *keyWork) startScope() {
	w.scopeEntries, w.scopeSkipped = 0, false
}

// charge adds steps and reports whether the total now passes the limit.
func (w *keyWork) charge(steps int) bool {
	w.total += steps
	return w.total > w.limit
}

// plainKey records a plain entry of segs key segments in the current scope
// and reports whether the total passes the limit. In a skipped scope (see
// keyWork) the entry is never looked up and leaves no tracker entry, so it
// costs nothing and does not widen the scope.
func (w *keyWork) plainKey(segs int) bool {
	if w.scopeSkipped {
		return false
	}
	w.scopeEntries += segs
	w.maxEntries = max(w.maxEntries, w.scopeEntries)
	return w.charge(segs * (w.scopeEntries + len(w.headers)))
}

// inlineKey records an entry of segs key segments in an inline table that
// already holds *entries key segments.
func (w *keyWork) inlineKey(entries *int, segs int) bool {
	*entries += segs
	return w.charge(segs * *entries)
}

// header records a header of segs segments with the given text, brackets
// included, and settles whether the scope it opens is skipped.
func (w *keyWork) header(text []byte, segs int) bool {
	w.headers.add(text)
	w.scopeSkipped = w.unknownTable(text)
	return w.charge((segs + 1) * (len(w.headers) + w.maxEntries))
}

// unknownTable reports whether text is a header the decode target provably
// never enters: a single bare segment that names no known table. With no
// known tables there is nothing to prove against, and a dotted or quoted
// header might still name a known table under a spelling this pass does not
// decode, so both count as tracked. Misreading a header can therefore only
// over-charge, never skip a scope the decoder tracks.
func (w *keyWork) unknownTable(text []byte) bool {
	if len(w.knownTables) == 0 {
		return false
	}
	name, ok := bareHeaderName(text)
	return ok && !knownTable(w.knownTables, name)
}

// knownTable reports whether name is one of the known tables. The compare
// folds case because go-toml matches a header to a struct field
// case-insensitively: [[Package]] enters a field tagged "package" and its
// keys are tracker-checked, so a case variant must count as known. Bare
// keys are ASCII, so the fold is exact here. Known sets name a target's
// root tables and stay a handful of entries, so the scan is short.
func knownTable(tables map[string]bool, name string) bool {
	for known := range tables {
		if strings.EqualFold(name, known) {
			return true
		}
	}
	return false
}

// bareHeaderName returns the table name of a header written as one bare key
// segment, brackets and padding aside: "[metadata]", "[[package]]". Any
// other form (dotted, quoted, empty) reports false.
func bareHeaderName(text []byte) (string, bool) {
	for len(text) > 0 && text[0] == '[' {
		text = text[1:]
	}
	for len(text) > 0 && text[len(text)-1] == ']' {
		text = text[:len(text)-1]
	}
	for len(text) > 0 && (text[0] == ' ' || text[0] == '\t') {
		text = text[1:]
	}
	for len(text) > 0 && (text[len(text)-1] == ' ' || text[len(text)-1] == '\t') {
		text = text[:len(text)-1]
	}
	if len(text) == 0 {
		return "", false
	}
	for _, c := range text {
		if !isBareKeyByte(c) {
			return "", false
		}
	}
	return string(text), true
}

// isBareKeyByte reports whether c may appear in a TOML bare key.
func isBareKeyByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-'
}

// exceeded builds the typed error for a pass that went over the limit.
func (w *keyWork) exceeded(at int) error {
	return fmt.Errorf("%w: work %d passes limit %d at byte %d", ErrTooManyKeys, w.total, w.limit, at)
}

// headerSeed makes header hashes unpredictable, so a hostile file cannot
// craft colliding headers to hide distinct tables from the count.
var headerSeed = maphash.MakeSeed()

// headerSet holds the hashes of the distinct table headers seen so far. Its
// size is the number of distinct headers, which bounds the children any one
// table can hold through headers. It grows by one 8-byte hash per distinct
// header and the work limit caps it, because every header costs at least its
// own count.
type headerSet map[uint64]struct{}

// add reports whether text is a header not seen before and records it.
func (s *headerSet) add(text []byte) bool {
	if *s == nil {
		*s = make(headerSet)
	}
	sum := maphash.Bytes(headerSeed, text)
	if _, seen := (*s)[sum]; seen {
		return false
	}
	(*s)[sum] = struct{}{}
	return true
}

// CheckBounds is Check with a second bound. It runs the same linear pass and
// returns nil, an error wrapping ErrTooDeep, an error wrapping
// ErrTooManyKeys, or an error wrapping ErrUnterminatedString. A negative
// field behaves like zero.
//
// The key-work total estimates the steps go-toml's key lookup takes (see
// keyWork for the charges). A Cargo.lock of 50,000 packages costs about 1.6
// million (1,599,985 for the tests' 12.6 MB fixture) because each
// [[package]] header sees a handful of siblings, and the
// same header repeated after thousands of distinct ones costs thousands every
// time. Distinct headers are remembered as seeded 64-bit hashes so a hostile
// file cannot craft collisions that hide them.
//
// The estimate never falls below the steps of a step-counting copy of the
// go-toml tracker on the documents the tests generate. It may exceed them: a
// dotted key counts its segments against the whole scope, a header counts the
// distinct headers of the whole document as its siblings, and two spellings
// of one key count as two.
func CheckBounds(source []byte, b Bounds) error {
	_, err := scanBounds(source, b)
	return err
}
