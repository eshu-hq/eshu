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
// a caller that leaves one out fails closed.
type Bounds struct {
	// Depth is the deepest nesting allowed, as for Check.
	Depth int
	// KeyWork bounds the quadratic cost of the decoder's key lookup. A table
	// that holds n entries adds n squared, and the sum over every table in
	// the document may not pass KeyWork. See CheckBounds.
	KeyWork int
}

// keyWork is the running key-table work of one pass.
//
// The decoder's lookup cost for one table is the square of its entry count,
// so the document's cost is the sum of those squares. Each new entry of a
// table that already holds n-1 entries adds n squared minus (n-1) squared,
// which is 2n-1, so the total is kept without storing any table.
type keyWork struct {
	total int
	limit int
}

// add records one more entry in a table whose entry count is *n, and reports
// whether the total now passes the limit.
func (w *keyWork) add(n *int) bool {
	*n++
	w.total += 2**n - 1
	return w.total > w.limit
}

// exceeded builds the typed error for a pass that went over the limit.
func (w *keyWork) exceeded(at int) error {
	return fmt.Errorf("%w: work %d passes limit %d at byte %d", ErrTooManyKeys, w.total, w.limit, at)
}

// headerSeed makes header hashes unpredictable, so a hostile file cannot
// craft colliding headers to hide distinct tables from the count.
var headerSeed = maphash.MakeSeed()

// headerSet holds the hashes of the distinct table headers seen so far. A
// header such as [dependencies.serde] defines one entry of its parent table,
// and a repeated [[package]] defines none, so only the first sight of a
// header text counts. It grows by one 8-byte hash per distinct header and is
// capped by the work limit, because every distinct header adds to the work.
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
// Key work is counted in two places and added together. Every `=` that
// starts an entry adds to the table that holds it: the table under the latest
// header, or the inline table around it. Dotted keys count too, since each
// one is a separate `=`. Every distinct header text adds to one more count,
// standing for the parent table the header defines an entry in. A repeated
// header, as in a Cargo.lock with thousands of [[package]] sections, adds
// nothing, so a legal lockfile costs a few hundred units where a single table
// of 100,000 keys costs 10 billion. The count never falls below half the
// squared size of the largest table the decoder builds, because a table's
// entries come from `=` in one place and from header children, so its size
// squared is at most twice the sum of the two squared counts.
func CheckBounds(source []byte, b Bounds) error {
	_, err := scanBounds(source, b.Depth, b.KeyWork)
	return err
}
