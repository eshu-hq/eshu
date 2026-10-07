// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"slices"
	"sync"
	"time"
)

// lastRow holds the newest stored row a process has decoded, a fresh row it
// served or a stale one found under DecodeStale, as the writer stored it. It is bounded to one row per ModelReader and is
// safe for concurrent scrapes. The entries are kept unaged: the age is always
// derived at read time from the row's as_of and the database clock, so a held
// row can never be served as if it were fresh.
type lastRow struct {
	mu      sync.Mutex
	entries []Entry
	asOf    time.Time
	held    bool
}

// remember keeps entries stored at asOf unless a row at least as new is
// already held, so a slow scrape that finished late cannot move the held row
// back in time. The entries are copied.
func (l *lastRow) remember(entries []Entry, asOf time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.held && !asOf.After(l.asOf) {
		return
	}
	l.entries = slices.Clone(entries)
	l.asOf = asOf
	l.held = true
}

// newer reports whether a row at asOf would replace the held row, so the caller
// skips validating a row that remember would refuse.
func (l *lastRow) newer(asOf time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return !l.held || asOf.After(l.asOf)
}

// recall returns the held entries and their as_of; ok is false when the
// process has decoded no row yet. Entry values are immutable strings
// and AddAge never modifies its input, so the slice is shared.
func (l *lastRow) recall() (entries []Entry, asOf time.Time, ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.entries, l.asOf, l.held
}
