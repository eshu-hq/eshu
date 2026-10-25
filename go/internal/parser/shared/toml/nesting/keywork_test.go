// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nesting

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
)

// headerSetCeiling bounds the allocation of a rejection that came from distinct
// headers: the set holds one 8-byte hash per header, and the work limit stops
// it near sqrt(testWorkLimit) = 10,000 entries (about 0.6 MB measured).
const headerSetCeiling = 1 << 20

// testWorkLimit is the limit the unit tests use. Cargo's real limit lives in
// the rust package; this one only has to sit far above the legal documents
// below and far below the hostile ones.
const testWorkLimit = 100_000_000

// hostileTableDocs returns 100,000-entry documents that each hold the
// go-toml v2.3.1 key lookup for about 18 seconds, one per way of putting
// many entries into one table.
func hostileTableDocs(n int) map[string]string {
	build := func(head string, row func(i int) string) string {
		var b strings.Builder
		b.WriteString(head)
		for i := range n {
			b.WriteString(row(i))
		}
		return b.String()
	}
	return map[string]string{
		"table keys":        build("[dependencies]\n", func(i int) string { return fmt.Sprintf("k%d = \"1\"\n", i) }),
		"root keys":         build("", func(i int) string { return fmt.Sprintf("k%d = 1\n", i) }),
		"dotted keys":       build("", func(i int) string { return fmt.Sprintf("a.k%d = 1\n", i) }),
		"inline table keys": build("d = {", func(i int) string { return fmt.Sprintf("k%d = 1,", i) }) + "}\n",
		"inline dependency": build("[dependencies]\n", func(i int) string { return fmt.Sprintf("k%d = {version = \"1\"}\n", i) }),
		"subtable headers":  build("", func(i int) string { return fmt.Sprintf("[dependencies.k%d]\nversion = \"1\"\n", i) }),
		"root headers":      build("", func(i int) string { return fmt.Sprintf("[k%d]\nv = 1\n", i) }),
		"array headers":     build("", func(i int) string { return fmt.Sprintf("[[k%d]]\nv = 1\n", i) }),
	}
}

// TestCheckBoundsRejectsHostileTablesWithBoundedAllocation is the contract
// from #8991: a document with 100,000 entries in one table is rejected with
// a typed error before any decode, and the rejection allocates a bounded
// amount whatever the size. It measures bytes, not time.
func TestCheckBoundsRejectsHostileTablesWithBoundedAllocation(t *testing.T) {
	for name, doc := range hostileTableDocs(100_000) {
		t.Run(name, func(t *testing.T) {
			src := []byte(doc)
			var err error
			got := allocatedBytes(func() { err = CheckBounds(src, Bounds{Depth: 64, KeyWork: testWorkLimit}) })
			if !errors.Is(err, ErrTooManyKeys) {
				t.Fatalf("CheckBounds() = %v, want ErrTooManyKeys", err)
			}
			ceiling := uint64(guardAllocCeiling)
			if strings.Contains(name, "headers") {
				ceiling = headerSetCeiling
			}
			if got > ceiling {
				t.Fatalf("CheckBounds() allocated %d bytes per op, want <= %d", got, ceiling)
			}
		})
	}
}

// TestCheckBoundsStopsEarly proves the pass stops at the limit instead of
// scanning the whole hostile file: the work counter at rejection is within
// one entry of the limit.
func TestCheckBoundsStopsEarly(t *testing.T) {
	doc := []byte(hostileTableDocs(100_000)["table keys"])
	got, err := scanBounds(doc, 64, testWorkLimit)
	if !errors.Is(err, ErrTooManyKeys) {
		t.Fatalf("scanBounds() error = %v, want ErrTooManyKeys", err)
	}
	if got.work <= testWorkLimit || got.work > testWorkLimit+2*10_001 {
		t.Fatalf("work = %d at rejection, want just past %d", got.work, testWorkLimit)
	}
}

// TestCheckBoundsAcceptsLegalDocuments keeps the bound well clear of real
// Cargo files: a 50,000-package lockfile (12.6 MB of [[package]] sections),
// and a manifest with a 2,000-entry dependency table.
func TestCheckBoundsAcceptsLegalDocuments(t *testing.T) {
	var manifest strings.Builder
	manifest.WriteString("[package]\nname = \"x\"\nversion = \"0.1.0\"\n\n[dependencies]\n")
	for i := range 2000 {
		fmt.Fprintf(&manifest, "dep%d = { version = \"1\", features = [\"a\", \"b\"] }\n", i)
	}
	for name, doc := range map[string][]byte{
		"lockfile":         legalLockfile(50_000),
		"manifest":         []byte(manifest.String()),
		"repeated headers": []byte(strings.Repeat("[[bin]]\nname = \"b\"\n", 20_000)),
		"target tables":    targetTables(500),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := scanBounds(doc, 64, testWorkLimit)
			if err != nil {
				t.Fatalf("scanBounds() = %v, want nil (work %d)", err, got.work)
			}
		})
	}
}

// TestCheckBoundsCountsEveryTableForm pins the work total for each way an
// entry is defined, so a form that drops out of the count fails here.
func TestCheckBoundsCountsEveryTableForm(t *testing.T) {
	for _, tc := range []struct {
		name string
		doc  string
		work int
	}{
		{"root keys", "a=1\nb=2\nc=3\n", 9},
		{"keys reset at header", "[x]\na=1\nb=2\n[y]\nc=1\n", 4 + 1 + 4},
		{"dotted keys", "a.b=1\na.c=1\n", 4},
		{"inline table", "d = {a=1, b=2, c=3}\nz=1\n", 9 + 4},
		{"nested inline", "d = {a={b=1,c=2}}\n", 1 + 1 + 4},
		{"array of inline", "d = [{a=1},{a=2}]\n", 1 + 1 + 1},
		{"distinct headers", "[a]\n[b]\n[c]\n", 9},
		{"repeated array header", "[[p]]\n[[p]]\n[[p]]\n", 1},
		{"string and comment", "a = \"b = 1\" # c = 2\n", 1},
		{"header comment", "[a] # [b]\nx=1\n", 1 + 1},
		{"header at eof", "[a]\n[b]", 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := scanBounds([]byte(tc.doc), math.MaxInt, math.MaxInt)
			if err != nil {
				t.Fatalf("scanBounds() = %v", err)
			}
			if got.work != tc.work {
				t.Fatalf("work = %d, want %d", got.work, tc.work)
			}
		})
	}
}

// TestBoundsZeroFailsClosed: a caller that leaves a bound out gets the
// strictest reading, never an unlimited one.
func TestBoundsZeroFailsClosed(t *testing.T) {
	if err := CheckBounds([]byte("a = 1\n"), Bounds{Depth: 64}); !errors.Is(err, ErrTooManyKeys) {
		t.Fatalf("CheckBounds(zero KeyWork) = %v, want ErrTooManyKeys", err)
	}
	if err := CheckBounds([]byte("a = [1]\n"), Bounds{KeyWork: 10}); !errors.Is(err, ErrTooDeep) {
		t.Fatalf("CheckBounds(zero Depth) = %v, want ErrTooDeep", err)
	}
	if err := CheckBounds([]byte("a = 1\n"), Bounds{Depth: -3, KeyWork: -3}); !errors.Is(err, ErrTooManyKeys) {
		t.Fatalf("CheckBounds(negative) = %v, want ErrTooManyKeys", err)
	}
}

// targetTables returns n [target.'cfg(...)'.dependencies] sections of three
// keys each, the widest legal Cargo manifest layout.
func targetTables(n int) []byte {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "[target.'cfg(x%d)'.dependencies]\na = \"1\"\nb = \"1\"\nc = \"1\"\n", i)
	}
	return []byte(b.String())
}
