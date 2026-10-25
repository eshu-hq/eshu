// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nesting

import (
	"errors"
	"fmt"
	"hash/maphash"
)

// ErrTooManyKeys reports a document whose tables hold so many keys that a
// decoder's key lookup would run for far longer than any real file needs.
// go-toml v2.3.1 finds a key by scanning a linked list, so decoding a table
// of n keys costs n squared steps: 100,000 keys under one [dependencies]
// header hold a parse worker for about 18 seconds. Callers treat the file as
// unparseable, the same as ErrTooDeep.
var ErrTooManyKeys = errors.New("toml key-table work exceeds limit")

// Bounds are the limits CheckBounds enforces. A zero field allows nothing, so
// a caller that leaves one out fails closed, except StructFields where zero is
// the strictest reading (every entry is looked up).
type Bounds struct {
	// Depth is the deepest nesting allowed, as for Check.
	Depth int
	// KeyWork bounds the decoder's key-lookup steps. Each lookup is charged
	// the length of the sibling list it scans, so the total tracks the steps
	// go-toml takes. See CheckBounds.
	KeyWork int
	// StructFields is zero when the caller decodes into a map, so go-toml
	// looks up every entry. When the caller decodes into a struct it is the
	// largest field count of any struct in the target. go-toml then stops
	// looking up the plain entries of a table after the first key the struct
	// does not know, so at most StructFields+1 entries per table are charged.
	// Inline tables are charged in full either way.
	StructFields int
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
type keyWork struct {
	total int
	limit int
	// structFields is Bounds.StructFields.
	structFields int
	// scopeEntries and scopeKeys describe the table under the latest header:
	// the key segments its plain entries defined, and how many entries.
	scopeEntries int
	scopeKeys    int
	// maxEntries is the largest scopeEntries any scope has reached.
	maxEntries int
	headers    headerSet
}

// startScope begins the table under a new header.
func (w *keyWork) startScope() {
	w.scopeEntries, w.scopeKeys = 0, 0
}

// charge adds steps and reports whether the total now passes the limit.
func (w *keyWork) charge(steps int) bool {
	w.total += steps
	return w.total > w.limit
}

// plainKey records a plain entry of segs key segments in the current scope and
// reports whether the total passes the limit. A struct target stops looking
// up entries after structFields+1 of them in one scope, so the rest are free.
func (w *keyWork) plainKey(segs int) bool {
	w.scopeKeys++
	if w.structFields > 0 && w.scopeKeys > w.structFields+1 {
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

// header records a header of segs segments with the given text.
func (w *keyWork) header(text []byte, segs int) bool {
	w.headers.add(text)
	return w.charge((segs + 1) * (len(w.headers) + w.maxEntries))
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
// keyWork for the charges). A Cargo.lock of 50,000 packages costs about 1.2
// million because each [[package]] header sees a handful of siblings, and the
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
