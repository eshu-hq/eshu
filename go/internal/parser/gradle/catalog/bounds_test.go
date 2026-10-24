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
	// Past the cap an entry costs only its parse, so the total tracks MaxRows
	// and not the entry count.
	requireCeiling(t, "bytes allocated for 80k entries", bytesAllocated, MaxRows*3072+uint64(len(source))/4)

	half := catalogWithEntries(40_000)
	halfBytes, _ := allocOf(func() { Scan(half) })
	if ratio := float64(bytesAllocated) / float64(halfBytes); ratio > 1.25 {
		t.Fatalf("doubling entries past the cap grew allocation %.2fx, want <= 1.25x", ratio)
	}
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
