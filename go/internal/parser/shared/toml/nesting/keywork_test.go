// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nesting

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2/unstable"
)

// headerSetCeiling bounds the allocation of a rejection that came from distinct
// headers: the set holds one 8-byte hash per header, and the work limit stops
// it near sqrt(testWorkLimit) = 10,000 entries (about 0.6 MB measured).
const headerSetCeiling = 1 << 20

// testWorkLimit is the limit the unit tests use. Cargo's real limit lives in
// the rust package; this one only has to sit far above the legal documents
// below and far below the hostile ones.
const testWorkLimit = 25_000_000

// testBounds are the bounds the unit tests apply.
var testBounds = Bounds{Depth: 64, KeyWork: testWorkLimit}

// lockStructBounds are the bounds for a lockfile decoded into a struct whose
// widest element has four fields.
var lockStructBounds = Bounds{Depth: 64, KeyWork: testWorkLimit, StructFields: 4}

// buildInline joins n comma-separated entries into one inline table. Every
// hostile document must be valid TOML, or the guard is not what stands
// between the file and a quadratic decode: go-toml rejects a trailing comma
// (`k9 = 1,}`) without any key lookup.
func buildInline(head string, n int) string {
	var b strings.Builder
	b.WriteString(head)
	for i := range n {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "k%d = 1", i)
	}
	return b.String()
}

// hostileTableDocs returns n-entry documents that each hold the go-toml
// v2.3.1 key lookup (about 18 seconds at the full 100,000 scale), one per
// way of putting many entries into one table. The entry count comes from
// hostileTableEntries so the race detector runs a smaller but still
// over-limit shape.
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
		"inline table keys": buildInline("d = {", n) + "}\n",
		"inline dependency": build("[dependencies]\n", func(i int) string { return fmt.Sprintf("k%d = {version = \"1\"}\n", i) }),
		"subtable headers":  build("", func(i int) string { return fmt.Sprintf("[dependencies.k%d]\nversion = \"1\"\n", i) }),
		"root headers":      build("", func(i int) string { return fmt.Sprintf("[k%d]\nv = 1\n", i) }),
		"array headers":     build("", func(i int) string { return fmt.Sprintf("[[k%d]]\nv = 1\n", i) }),
	}
}

// repeatedHeaderDocs are the layouts from the #8991 review: headers that
// name an already-seen table cost the decoder a scan of the siblings every
// time, so they are linear in the repeat count and not bounded by distinct
// header texts. distinct is the number of sibling tables, repeats the number
// of times the array table comes back, and elemKeys the keys each repeated
// [[x.y]] element carries. Bare repeats of an empty array child cost the
// decoder almost nothing per repeat (each lookup hits a one-entry list), so
// the third layout carries keys: rebuilding each element's children is what
// makes the repeats rescan.
func repeatedHeaderDocs(distinct, repeats, elemKeys int) map[string]string {
	var rootSiblings, childSiblings strings.Builder
	rootSiblings.WriteString("[[package]]\nname = \"a\"\n")
	for i := range distinct {
		fmt.Fprintf(&rootSiblings, "[c%d]\n", i)
	}
	rootSiblings.WriteString(strings.Repeat("[[package]]\nname = \"a\"\n", repeats))
	for range repeats {
		childSiblings.WriteString("[[package]]\nname = \"a\"\n")
		for i := range distinct {
			fmt.Fprintf(&childSiblings, "[package.c%d]\n", i)
		}
	}
	var keysThenChildren strings.Builder
	keysThenChildren.WriteString("[x]\n")
	for i := range distinct {
		fmt.Fprintf(&keysThenChildren, "k%d = 1\n", i)
	}
	for range repeats {
		keysThenChildren.WriteString("[[x.y]]\n")
		for i := range elemKeys {
			fmt.Fprintf(&keysThenChildren, "k%d = 1\n", i)
		}
	}
	return map[string]string{
		"array header after root siblings": rootSiblings.String(),
		"array element rebuilds children":  childSiblings.String(),
		"array child after table keys":     keysThenChildren.String(),
	}
}

// TestCheckBoundsRejectsHostileTablesWithBoundedAllocation is the contract
// from #8991: a document with 100,000 entries in one table is rejected with
// a typed error before any decode, and the rejection allocates a bounded
// amount whatever the size. It measures bytes, not time.
func TestCheckBoundsRejectsHostileTablesWithBoundedAllocation(t *testing.T) {
	for name, doc := range hostileTableDocs(hostileTableEntries()) {
		t.Run(name, func(t *testing.T) {
			src := []byte(doc)
			var err error
			got := allocatedBytes(func() { err = CheckBounds(src, testBounds) })
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

// TestCheckBoundsRejectsRepeatedHeaders is the P1 from the #8991 review. Each
// layout stays under the limit while the repeats are few, passes the old
// distinct-header count at any size, and is rejected once the repeats make the
// decoder rescan the siblings thousands of times. The document is converted
// once outside the measured closure: converting inside would charge the copy
// of the multi-megabyte file to the guard.
func TestCheckBoundsRejectsRepeatedHeaders(t *testing.T) {
	for name, doc := range repeatedHeaderDocs(repeatScale()) {
		t.Run(name, func(t *testing.T) {
			src := []byte(doc)
			var err error
			got := allocatedBytes(func() { err = CheckBounds(src, testBounds) })
			if !errors.Is(err, ErrTooManyKeys) {
				t.Fatalf("CheckBounds() = %v, want ErrTooManyKeys", err)
			}
			if got > headerSetCeiling {
				t.Fatalf("CheckBounds() allocated %d bytes per op, want <= %d", got, headerSetCeiling)
			}
		})
	}
}

// TestHostileTableDocsAreValidTOML keeps the hostile documents honest: each
// must parse, so the guard is what stands between the file and a quadratic
// decode. An invalid document is rejected by the decoder without any key
// lookup, which makes its rejection prove nothing.
func TestHostileTableDocsAreValidTOML(t *testing.T) {
	for name, doc := range hostileTableDocs(10) {
		t.Run(name, func(t *testing.T) {
			var p unstable.Parser
			p.Reset([]byte(doc))
			for p.NextExpression() {
			}
			if err := p.Error(); err != nil {
				t.Fatalf("parser error = %v, want nil", err)
			}
		})
	}
}

// TestCheckBoundsStopsEarly proves the pass stops at the limit instead of
// scanning the whole hostile file: the work counter at rejection is within
// one entry of the limit.
func TestCheckBoundsStopsEarly(t *testing.T) {
	doc := []byte(hostileTableDocs(hostileTableEntries())["table keys"])
	got, err := scanBounds(doc, testBounds)
	if !errors.Is(err, ErrTooManyKeys) {
		t.Fatalf("scanBounds() error = %v, want ErrTooManyKeys", err)
	}
	if got.work <= testWorkLimit || got.work > testWorkLimit+10_001 {
		t.Fatalf("work = %d at rejection, want just past %d", got.work, testWorkLimit)
	}
}

// TestCheckBoundsAcceptsLegalDocuments keeps the bound well clear of real
// Cargo files: a 50,000-package lockfile (12.6 MB of [[package]] sections),
// a manifest with a 2,000-entry dependency table, and many target tables.
func TestCheckBoundsAcceptsLegalDocuments(t *testing.T) {
	var manifest strings.Builder
	manifest.WriteString("[package]\nname = \"x\"\nversion = \"0.1.0\"\n\n[dependencies]\n")
	for i := range 2000 {
		fmt.Fprintf(&manifest, "dep%d = { version = \"1\", features = [\"a\", \"b\"] }\n", i)
	}
	for name, tc := range map[string]struct {
		doc    []byte
		bounds Bounds
	}{
		"lockfile":          {legalLockfile(50_000), lockStructBounds},
		"v1 lockfile":       {legalLockfileV1(50_000), lockStructBounds},
		"manifest":          {[]byte(manifest.String()), testBounds},
		"repeated headers":  {[]byte(strings.Repeat("[[bin]]\nname = \"b\"\n", 20_000)), testBounds},
		"target tables":     {targetTables(500), testBounds},
		"dotted dependency": {[]byte("[dependencies]\nserde.version = \"1\"\nserde.features = [\"derive\"]\n"), testBounds},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := scanBounds(tc.doc, tc.bounds)
			if err != nil {
				t.Fatalf("scanBounds() = %v, want nil (work %d)", err, got.work)
			}
		})
	}
}

// TestStructBoundsSkipPlainEntriesAfterTheFirstUnknownKey is the P2 from the
// #8991 review. A v1 Cargo.lock has one `"checksum ..."` key per package under
// [metadata]. go-toml decoding into the lock struct skips that table's plain
// entries untracked, so a struct caller is not charged for them, and a map
// caller (every entry looked up) is.
func TestStructBoundsSkipPlainEntriesAfterTheFirstUnknownKey(t *testing.T) {
	doc := legalLockfileV1(50_000)
	if _, err := scanBounds(doc, lockStructBounds); err != nil {
		t.Fatalf("struct bounds: scanBounds() = %v, want nil", err)
	}
	if _, err := scanBounds(doc, testBounds); !errors.Is(err, ErrTooManyKeys) {
		t.Fatalf("map bounds: scanBounds() = %v, want ErrTooManyKeys", err)
	}
}

// TestStructBoundsStillChargeInlineTablesAndHeaders keeps the struct reading
// from becoming a hole: inline tables and headers are looked up in full.
func TestStructBoundsStillChargeInlineTablesAndHeaders(t *testing.T) {
	docs := hostileTableDocs(hostileTableEntries())
	for _, name := range []string{"inline table keys", "subtable headers", "root headers", "array headers"} {
		if _, err := scanBounds([]byte(docs[name]), lockStructBounds); !errors.Is(err, ErrTooManyKeys) {
			t.Errorf("%s: scanBounds() = %v, want ErrTooManyKeys", name, err)
		}
	}
	// Bare repeats of an empty array child are not rejected here: the struct
	// decode skips the unknown table's keys and each repeated lookup hits a
	// one-entry list, so the document is genuinely cheap and struct bounds
	// accept it. Map bounds still reject it as policy (see
	// TestCheckBoundsRejectsRepeatedHeaders), and
	// TestStructShapeWorkCoversTheDecoder proves the work still covers the
	// steps, so the acceptance is sound.
	layouts := repeatedHeaderDocs(repeatScale())
	for _, name := range []string{"array header after root siblings", "array element rebuilds children"} {
		if _, err := scanBounds([]byte(layouts[name]), lockStructBounds); !errors.Is(err, ErrTooManyKeys) {
			t.Errorf("%s: scanBounds() = %v, want ErrTooManyKeys", name, err)
		}
	}
	// The first entry of a table is looked up even when the struct does not
	// know it, so an inline table as that entry is charged.
	first := []byte("[metadata]\nx = {" + strings.Repeat("a=1,", hostileTableEntries()) + "}\n")
	if _, err := scanBounds(first, lockStructBounds); !errors.Is(err, ErrTooManyKeys) {
		t.Errorf("first entry inline table: scanBounds() = %v, want ErrTooManyKeys", err)
	}
}

// TestCheckBoundsCountsEveryTableForm pins the work total for each way a
// lookup happens, so a form that drops out of the count fails here. Each
// total is worked out in the case: a header of s segments costs
// (s+1) * (distinct headers + widest scope), a plain entry of m segments costs
// m * (its scope's segments + distinct headers), and an inline entry costs
// m * (its inline table's segments).
func TestCheckBoundsCountsEveryTableForm(t *testing.T) {
	for _, tc := range []struct {
		name   string
		doc    string
		bounds Bounds
		work   int
	}{
		{"root keys", "a=1\nb=2\nc=3\n", testBounds, 1 + 2 + 3},
		{
			"keys reset at header", "[x]\na=1\nb=2\n[y]\nc=1\n", testBounds,
			2*1 + // [x]: 1 distinct, widest scope 0
				1*(1+1) + 1*(2+1) + // a, b: scope entries so far plus 1 distinct
				2*(2+2) + // [y]: 2 distinct, widest scope 2
				1*(1+2),
		}, // c
		{"dotted keys", "a.b=1\na.c=1\n", testBounds, 2*2 + 2*4},
		{"inline table", "d = {a=1, b=2, c=3}\nz=1\n", testBounds, 1 + (1 + 2 + 3) + 2},
		{"nested inline", "d = {a={b=1,c=2}}\n", testBounds, 1 + 1 + (1 + 2)},
		{"array of inline", "d = [{a=1},{a=2}]\n", testBounds, 1 + 1 + 1},
		{"distinct headers", "[a]\n[b]\n[c]\n", testBounds, 2*1 + 2*2 + 2*3},
		{"repeated array header", "[[p]]\n[[p]]\n[[p]]\n", testBounds, 3 * 2 * 1},
		{"dotted header", "[a.b.c]\n", testBounds, 4 * 1},
		{"string and comment", "a = \"b = 1\" # c = 2\n", testBounds, 1},
		{"header comment", "[a] # [b]\nx=1\n", testBounds, 2*1 + 1*(1+1)},
		{"header at eof", "[a]\n[b]", testBounds, 2*1 + 2*2},
		{"struct stops after the first unknown", "a=1\nb=2\nc=3\nd=4\n", Bounds{Depth: 64, KeyWork: 1000, StructFields: 2}, 1 + 2 + 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.bounds.KeyWork = math.MaxInt
			got, err := scanBounds([]byte(tc.doc), tc.bounds)
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

// legalLockfileV1 returns a Cargo.lock v1 with n packages: each has a source,
// and [metadata] holds one checksum key per package.
func legalLockfileV1(n int) []byte {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "[[package]]\nname = \"crate-%d\"\nversion = \"1.0.0\"\nsource = \"registry+https://github.com/rust-lang/crates.io-index\"\n\n", i)
	}
	b.WriteString("[metadata]\n")
	for i := range n {
		fmt.Fprintf(&b, "\"checksum crate-%d 1.0.0 (registry+https://github.com/rust-lang/crates.io-index)\" = \"%064x\"\n", i, i)
	}
	return []byte(b.String())
}
