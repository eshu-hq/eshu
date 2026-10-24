// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import (
	"bytes"
	"fmt"
	"runtime"
	"strings"
	"testing"
)

// allocOf runs fn and returns the bytes and objects it allocated. It reads the
// process-wide counters, so the callers are serial tests: Go holds parallel
// tests until every serial test has finished.
func allocOf(fn func()) (bytesAllocated, objects uint64) {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc, after.Mallocs - before.Mallocs
}

// requireCeiling fails when got exceeds limit. Ceilings are allocation sizes
// relative to the input length, never wall time, so a slow shared runner does
// not flake them.
func requireCeiling(t *testing.T, what string, got, limit uint64) {
	t.Helper()
	t.Logf("%s = %d (ceiling %d)", what, got, limit)
	if got > limit {
		t.Fatalf("%s = %d, want <= %d", what, got, limit)
	}
}

func catalogWithEntries(n int) []byte {
	var b strings.Builder
	b.WriteString("[libraries]\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "a%d = \"g:a%d:1\"\n", i, i)
	}
	return []byte(b.String())
}

func TestScanOversizeInputIsRefusedWithoutParsing(t *testing.T) {
	source := append([]byte("a = \""), bytes.Repeat([]byte("x"), 10<<20)...)
	var result Result
	bytesAllocated, _ := allocOf(func() { result = Scan(source) })
	if result.State != StateOversize || len(result.Rows) != 0 {
		t.Fatalf("10 MB single line: State=%q rows=%d, want oversize with none", result.State, len(result.Rows))
	}
	requireCeiling(t, "bytes allocated for a 10 MB line", bytesAllocated, 16<<10)
}

func TestScanRowCapCountsTheExcess(t *testing.T) {
	source := catalogWithEntries(80_000)
	if len(source) > MaxSourceBytes {
		t.Fatalf("test input %d bytes exceeds MaxSourceBytes %d: shrink the entry count", len(source), MaxSourceBytes)
	}
	var result Result
	bytesAllocated, _ := allocOf(func() { result = Scan(source) })
	if result.State != StateParsed || len(result.Rows) != MaxRows {
		t.Fatalf("80k entries: State=%q rows=%d, want parsed with %d", result.State, len(result.Rows), MaxRows)
	}
	if want := 80_000 - MaxRows; result.Counts.Dropped != want {
		t.Fatalf("Dropped = %d, want %d", result.Counts.Dropped, want)
	}
	// Past the cap an entry costs its parse and one key-tree entry (about 70
	// bytes, measured), never a row (about 3 KB), so the total tracks MaxRows
	// plus a small per-entry term.
	requireCeiling(t, "bytes allocated for 80k entries", bytesAllocated, MaxRows*3072+uint64(len(source))/4)

	half := catalogWithEntries(40_000)
	halfBytes, _ := allocOf(func() { Scan(half) })
	perEntry := (int64(bytesAllocated) - int64(halfBytes)) / 40_000
	requireCeiling(t, "bytes allocated per entry past the cap", uint64(max(perEntry, 0)), 128)
}

func TestScanDeepNestingIsRefusedBeforeTheTOMLParserRuns(t *testing.T) {
	for _, open := range []string{"[", "{", "[{"} {
		source := []byte("x = ")
		source = append(source, bytes.Repeat([]byte(open), 1<<19)...)
		var result Result
		bytesAllocated, _ := allocOf(func() { result = Scan(source) })
		if result.State != StateTooDeep {
			t.Fatalf("%q x 1M: State=%q, want %q", open, result.State, StateTooDeep)
		}
		requireCeiling(t, "bytes allocated for 1M-deep nesting", bytesAllocated, 16<<10)
	}
}

func TestScanNestingBoundIsExactlyMaxNesting(t *testing.T) {
	ok := "x = " + strings.Repeat("[", MaxNesting) + strings.Repeat("]", MaxNesting) + "\n"
	if result := Scan([]byte(ok)); result.State == StateTooDeep {
		t.Fatalf("depth %d refused, want accepted", MaxNesting)
	}
	tooDeep := "x = " + strings.Repeat("[", MaxNesting+1) + strings.Repeat("]", MaxNesting+1) + "\n"
	if result := Scan([]byte(tooDeep)); result.State != StateTooDeep {
		t.Fatalf("depth %d State=%q, want %q", MaxNesting+1, result.State, StateTooDeep)
	}
}

func TestScanBracketsInsideStringsAndCommentsDoNotCountAsNesting(t *testing.T) {
	deep := strings.Repeat("[", 4*MaxNesting)
	for name, text := range map[string]string{
		"basic":     "[libraries]\na = \"" + deep + "\"\n",
		"literal":   "[libraries]\na = '" + deep + "'\n",
		"multiline": "[libraries]\na = \"\"\"\n" + deep + "\n\"\"\"\n",
		"comment":   "[libraries]\n# " + deep + "\na = \"g:a:1\"\n",
		"escaped":   "[libraries]\na = \"\\\"" + deep + "\"\nb = \"g:b:1\"\n",
	} {
		if result := Scan([]byte(text)); result.State == StateTooDeep {
			t.Errorf("%s: brackets in text counted as nesting", name)
		}
	}
}

func TestScanUnterminatedStringsStayLinear(t *testing.T) {
	fill := bytes.Repeat([]byte("x"), 1_900_000)
	for name, prefix := range map[string]string{
		"basic":             "a = \"",
		"literal":           "a = '",
		"multiline basic":   "a = \"\"\"",
		"multiline literal": "a = '''",
	} {
		source := append([]byte(prefix), fill...)
		var result Result
		bytesAllocated, _ := allocOf(func() { result = Scan(source) })
		if result.State != StateMalformed || len(result.Rows) != 0 {
			t.Fatalf("%s: State=%q rows=%d, want malformed with none", name, result.State, len(result.Rows))
		}
		requireCeiling(t, name+" bytes allocated", bytesAllocated, uint64(len(source))/32)
	}
}

func TestScanOneHugeValueStaysWithinTheSourceBound(t *testing.T) {
	source := []byte("[libraries]\na = \"" + strings.Repeat("x", 1_900_000) + "\"\n")
	var result Result
	bytesAllocated, _ := allocOf(func() { result = Scan(source) })
	if result.State != StateParsed || len(result.Rows) != 0 || result.Counts.Unsupported != 1 {
		t.Fatalf("huge value: State=%q rows=%d counts=%+v, want parsed, no rows, one unsupported",
			result.State, len(result.Rows), result.Counts)
	}
	requireCeiling(t, "bytes allocated for a 1.9 MB value", bytesAllocated, uint64(len(source))/32)
}

func TestScanOverlongFieldIsUnsupportedNotTruncated(t *testing.T) {
	long := strings.Repeat("v", MaxFieldBytes+1)
	for name, text := range map[string]string{
		"version":  "[libraries]\na = { module = \"g:a\", version = \"" + long + "\" }\n",
		"group":    "[libraries]\na = \"" + long + ":a:1\"\n",
		"alias":    "[libraries]\n" + long + " = \"g:a:1\"\n",
		"notation": "[libraries]\na = \"g:a:" + long + "\"\n",
	} {
		result := Scan([]byte(text))
		if len(result.Rows) != 0 {
			// A notation version is not size-checked at read time; it must still
			// never come out as a resolved, truncated value.
			row := result.Rows[0]
			if row["dependency_resolution_state"] == "resolved" {
				t.Errorf("%s: overlong field produced a resolved row %v", name, row)
			}
			continue
		}
		if result.Counts.Unsupported != 1 {
			t.Errorf("%s: Counts = %+v, want one unsupported entry", name, result.Counts)
		}
	}
}

func TestScanAllocationGrowsLinearlyWithUnsupportedEntries(t *testing.T) {
	build := func(n int) []byte {
		var b strings.Builder
		b.WriteString("[libraries]\n")
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, "a%d = 3\n", i)
		}
		return []byte(b.String())
	}
	small, large := build(20_000), build(40_000)
	_, smallObjects := allocOf(func() { Scan(small) })
	_, largeObjects := allocOf(func() { Scan(large) })
	t.Logf("objects: n=20k %d, n=40k %d", smallObjects, largeObjects)
	if ratio := float64(largeObjects) / float64(smallObjects); ratio > 2.3 {
		t.Fatalf("doubling the input grew allocations %.2fx, want <= 2.3x (linear)", ratio)
	}
}

func TestScanInputEndingInsideATokenTerminates(t *testing.T) {
	brackets := strings.Repeat("[", 1<<18)
	cases := map[string]struct {
		text  string
		state string
	}{
		"comment at EOF":                   {"[libraries]\na = \"g:a:1\"\n# no newline", StateParsed},
		"comment holding brackets at EOF":  {"# " + brackets, StateParsed},
		"unterminated multiline brackets":  {"a = \"\"\"" + brackets, StateMalformed},
		"unterminated literal multiline":   {"a = '''" + brackets, StateMalformed},
		"unterminated basic then brackets": {"a = \"" + brackets, StateMalformed},
		"lone quote at EOF":                {"a = \"", StateMalformed},
		"escape at EOF":                    {"a = \"\\", StateMalformed},
	}
	for name, tc := range cases {
		result := Scan([]byte(tc.text))
		if result.State != tc.state {
			t.Errorf("%s: State=%q (%s), want %q", name, result.State, result.ParseError, tc.state)
		}
	}
}

func TestScanNestingIsCountedAfterAClosedString(t *testing.T) {
	deep := "x = " + strings.Repeat("[", MaxNesting+1) + strings.Repeat("]", MaxNesting+1) + "\n"
	for name, prefix := range map[string]string{
		"basic":             "a = \"s\"\n",
		"literal":           "a = 's'\n",
		"multiline basic":   "a = \"\"\"s\"\"\"\n",
		"multiline literal": "a = '''s'''\n",
		"comment":           "# c\n",
	} {
		if result := Scan([]byte(prefix + deep)); result.State != StateTooDeep {
			t.Errorf("%s: State=%q, want %q: a closed string must not hide later nesting", name, result.State, StateTooDeep)
		}
	}
}

// TestNestingGuardAgreesWithKnownDepthAcrossStringForms nests arrays to a known
// depth, puts every string form (with brackets, quotes and the four and five
// quote closings) at every level, and checks the guard against the depth the
// generator built. A lexer that disagrees with the TOML parser about a string
// boundary would hide or invent brackets here.
func TestNestingGuardAgreesWithKnownDepthAcrossStringForms(t *testing.T) {
	forms := []string{
		`"[[["`, `'[[['`, `"]]]"`, `"\"[["`, `"a\\"`, `""`, `''`,
		`"""[[[ """`, `"""x""""`, `"""x"""""`, `'''[[ '''`, `'''x''''`, `'''x'''''`,
		"\"\"\"\n[[[\n\"\"\"", `"""\"""[["""`,
	}
	for depth := 1; depth <= MaxNesting+3; depth++ {
		for _, form := range forms {
			for _, comment := range []string{"", " # [[[[[[[[[[[[[[[[[[[[[[[["} {
				var b strings.Builder
				b.WriteString("x = ")
				for i := 0; i < depth; i++ {
					b.WriteString("[" + form + ",")
				}
				b.WriteString(strings.Repeat("]", depth))
				b.WriteString(comment + "\nafter = 1\n")
				source := []byte(b.String())
				want := depth > MaxNesting
				if got := nestingExceeds(source, MaxNesting); got != want {
					t.Fatalf("depth %d form %q comment %q: nestingExceeds = %v, want %v\n%s", depth, form, comment, got, want, source)
				}
			}
		}
	}
}

// TestScanFourQuoteClosingCannotHideNesting is the regression for a guard
// bypass: a multi-line string closed by four quotes (content ending in a quote)
// left the lexer inside a phantom string for the rest of the line, so brackets
// on that line were not counted and the TOML parser received deep input.
func TestScanFourQuoteClosingCannotHideNesting(t *testing.T) {
	source := []byte("x = " + strings.Repeat(`["""x"""",`, 1<<16) + "\n")
	var result Result
	bytesAllocated, _ := allocOf(func() { result = Scan(source) })
	if result.State != StateTooDeep {
		t.Fatalf("State=%q, want %q", result.State, StateTooDeep)
	}
	requireCeiling(t, "bytes allocated", bytesAllocated, 16<<10)
}

// A 900,000-segment dotted key must stay bounded: the streaming parse builds
// one node per segment, and the scanner refuses the key before the key tree.
// The semantic TOML decode this package used earlier allocated about 400
// bytes per input byte on this shape; the streaming parse alone measured 139.
func TestScanLongDottedKeyAllocationStaysBounded(t *testing.T) {
	key := "a" + strings.Repeat(".a", 900_000)
	for name, text := range map[string]string{
		"root":   key + " = 1\n",
		"header": "[" + key + "]\nb = 1\n",
		"inline": "[libraries]\nx = { " + key + " = 1 }\n",
	} {
		source := []byte(text)
		var result Result
		bytesAllocated, _ := allocOf(func() { result = Scan(source) })
		if result.State != StateTooDeep {
			t.Errorf("%s: State=%q, want %q", name, result.State, StateTooDeep)
		}
		requireCeiling(t, name+" bytes allocated", bytesAllocated, uint64(len(source))*160)
	}
}

// budgetFile writes a root key whose inline table holds one key of
// inlineSegments segments, then headers of MaxKeySegments segments each, so
// the file totals 1 + inlineSegments + headers*MaxKeySegments key segments.
func budgetFile(inlineSegments, headers int) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "z = { a%s = 1 }\n", strings.Repeat(".a", inlineSegments-1))
	for i := 0; i < headers; i++ {
		fmt.Fprintf(&b, "[k%d%s]\n", i, strings.Repeat(".a", MaxKeySegments-1))
	}
	return []byte(b.String())
}

// The key segments of a whole file, inline-table keys included, are bounded:
// the streaming parse and the key tree allocate per segment. The worst file
// inside the bound, unique 64-segment headers up to it, is measured here.
func TestScanFileKeySegmentBoundIsExactlyMaxFileKeySegments(t *testing.T) {
	headers := MaxFileKeySegments/MaxKeySegments - 1
	at := budgetFile(MaxKeySegments-1, headers)
	over := budgetFile(MaxKeySegments, headers)
	var result Result
	bytesAllocated, _ := allocOf(func() { result = Scan(at) })
	if result.State != StateParsed {
		t.Fatalf("at the bound: State=%q (%s), want parsed", result.State, result.ParseError)
	}
	requireCeiling(t, "bytes allocated at the file key-segment bound", bytesAllocated, 64<<20)
	if result := Scan(over); result.State != StateTooDeep || result.ParseError != reasonFileSegments {
		t.Fatalf("one over the bound: State=%q (%s), want too_deep (%s)", result.State, result.ParseError, reasonFileSegments)
	}
}
