// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pythondep

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
)

// measureUVLockAllocs parses source once and returns the allocation deltas
// plus the parse result. The measurement covers only parseUVLockBytes so the
// ceilings below are relative to input size, never wall time.
func measureUVLockAllocs(source []byte) (uint64, uint64, uvLockResult) {
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	result := parseUVLockBytes(source)
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc, after.Mallocs - before.Mallocs, result
}

// assertLinearScan fails when the scanner examined more than three times the input
// bytes, which is how a quadratic rescan would show up without a stopwatch.
func assertLinearScan(t *testing.T, name string, result uvLockResult, size int) {
	t.Helper()
	if result.Scanned > 3*size+1024 {
		t.Fatalf("%s: scanned %d bytes for a %d byte input; the scan must stay linear", name, result.Scanned, size)
	}
}

// TestParseUVLockTenMegabyteSingleLineStaysBounded proves one huge line is
// neither copied nor rescanned: allocations stay under 1 MiB for 10 MiB.
func TestParseUVLockTenMegabyteSingleLineStaysBounded(t *testing.T) {
	cases := map[string]string{
		"giant name value":       "version = 1\n\n[[package]]\nname = \"" + strings.Repeat("a", 10<<20),
		"giant array line":       "version = 1\n\n[[package]]\nname = \"x\"\nversion = \"1.0\"\nsource = { registry = \"https://pypi.org/simple\" }\ndependencies = [" + strings.Repeat("{ name = \"a\" }, ", 10<<20/16),
		"giant unterminated str": "version = 1\n\n[[package]]\nname = \"x\"\nnote = \"" + strings.Repeat("b", 10<<20),
		"giant single line only": strings.Repeat("c", 10<<20),
		"giant header-like line": "[[package]]" + strings.Repeat(" ", 10<<20),
		"giant members line":     "version = 1\n[manifest]\nmembers = [" + strings.Repeat("\"member-name-x\", ", 10<<20/17) + "]\n",
		"giant distinct members": "version = 1\n[manifest]\nmembers = [" + distinctMembers(10<<20/12, ", ") + "]\n",
	}
	for name, body := range cases {
		source := []byte(body)
		bytesAlloc, _, result := measureUVLockAllocs(source)
		if bytesAlloc > 1<<20 {
			t.Fatalf("%s: allocated %d bytes for a %d byte input, want < 1 MiB", name, bytesAlloc, len(source))
		}
		assertLinearScan(t, name, result, len(source))
		for _, row := range result.Rows {
			if row["config_kind"] == "dependency" {
				t.Fatalf("%s: a truncated or oversized entry resolved: %#v", name, row)
			}
		}
	}
}

// TestParseUVLockHundredThousandEntriesAllocatesLikeTheRowCap proves entries
// beyond the row cap are counted, not materialised.
func TestParseUVLockHundredThousandEntriesAllocatesLikeTheRowCap(t *testing.T) {
	build := func(n int) []byte {
		var b strings.Builder
		b.WriteString("version = 1\n")
		for i := 0; i < n; i++ {
			b.WriteString("\n[[package]]\nname = \"pkg\"\nversion = \"1.0.0\"\nsource = { registry = \"https://pypi.org/simple\" }\n")
		}
		return []byte(b.String())
	}
	capBytes, capMallocs, capResult := measureUVLockAllocs(build(uvLockMaxRows))
	bigSource := build(100_000)
	bigBytes, bigMallocs, bigResult := measureUVLockAllocs(bigSource)

	if len(bigResult.Rows) != uvLockMaxRows || bigResult.State.Overflow != 100_000-uvLockMaxRows || bigResult.State.Entries != 100_000 {
		t.Fatalf("big state = %+v rows=%d, want cap rows and counted overflow", bigResult.State, len(bigResult.Rows))
	}
	if len(capResult.Rows) != uvLockMaxRows {
		t.Fatalf("cap-sized input rows = %d, want %d", len(capResult.Rows), uvLockMaxRows)
	}
	if bigBytes > capBytes+capBytes/10+4096 || bigMallocs > capMallocs+capMallocs/10+64 {
		t.Fatalf("100k entries allocated %d B / %d mallocs vs %d B / %d at the cap; overflow must not materialise rows", bigBytes, bigMallocs, capBytes, capMallocs)
	}
	assertLinearScan(t, "100k entries", bigResult, len(bigSource))
}

// TestParseUVLockDeepNestingStaysBounded proves bracket depth is a counter,
// not recursion or a stack.
func TestParseUVLockDeepNestingStaysBounded(t *testing.T) {
	const depth = 5 << 20
	source := []byte("version = 1\n\n[[package]]\nname = \"x\"\nversion = \"1.0\"\nsource = { registry = \"https://pypi.org/simple\" }\ndependencies = " + strings.Repeat("[", depth) + "\n" + strings.Repeat("]", depth) + "\n")
	bytesAlloc, _, result := measureUVLockAllocs(source)
	if bytesAlloc > 1<<20 {
		t.Fatalf("allocated %d bytes for %d nested brackets, want < 1 MiB", bytesAlloc, depth)
	}
	assertLinearScan(t, "deep nesting", result, len(source))
	if len(result.Rows) != 1 || result.Rows[0]["config_kind"] != "dependency" {
		t.Fatalf("balanced nesting should still resolve the entry: %#v", result.Rows)
	}
}

// TestParseUVLockUnterminatedStringsStayBounded proves many unterminated
// strings cost one pass.
func TestParseUVLockUnterminatedStringsStayBounded(t *testing.T) {
	var b strings.Builder
	b.WriteString("version = 1\n\n[[package]]\nname = \"x\"\nversion = \"1.0\"\nsource = { registry = \"https://pypi.org/simple\" }\nwheels = [\n")
	for i := 0; i < 200_000; i++ {
		b.WriteString("    \"unterminated\n")
	}
	source := []byte(b.String())
	bytesAlloc, _, result := measureUVLockAllocs(source)
	if bytesAlloc > 1<<20 {
		t.Fatalf("allocated %d bytes, want < 1 MiB", bytesAlloc)
	}
	assertLinearScan(t, "unterminated strings", result, len(source))
	for _, row := range result.Rows {
		if row["config_kind"] == "dependency" {
			t.Fatalf("entry with an unterminated string resolved: %#v", row)
		}
	}
}

// TestParseUVLockManyHeadersWithoutPackagesStaysBounded covers sub-table
// floods that carry no [[package]] header.
func TestParseUVLockManyHeadersWithoutPackagesStaysBounded(t *testing.T) {
	source := []byte("version = 1\n" + strings.Repeat("[package.metadata]\nrequires-dist = []\n", 200_000))
	bytesAlloc, _, result := measureUVLockAllocs(source)
	if bytesAlloc > 1<<20 || len(result.Rows) != 0 {
		t.Fatalf("allocated %d bytes rows=%d, want < 1 MiB and none", bytesAlloc, len(result.Rows))
	}
	assertLinearScan(t, "header flood", result, len(source))
}

// distinctMembers writes n distinct quoted member names joined by sep.
func distinctMembers(n int, sep string) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "\"m-%d\"%s", i, sep)
	}
	return b.String()
}

// TestParseUVLockMillionMemberLinesStayBounded proves a members array of one
// million lines costs a bounded number of allocations: every listed name, not
// only each distinct one, counts against the cap, and a list past the cap is
// not trusted.
func TestParseUVLockMillionMemberLinesStayBounded(t *testing.T) {
	cases := map[string]string{
		"repeated": "version = 1\n[manifest]\nmembers = [\n" + strings.Repeat("    \"member-name-x\",\n", 1_000_000) + "]\n",
		"distinct": "version = 1\n[manifest]\nmembers = [\n" + distinctMembers(1_000_000, ",\n") + "]\n",
	}
	for name, body := range cases {
		source := []byte(body + "\n[[package]]\nname = \"m-1\"\nversion = \"1.0\"\nsource = { editable = \"m\" }\n")
		bytesAlloc, mallocs, result := measureUVLockAllocs(source)
		if bytesAlloc > 1<<20 || mallocs > 40_000 {
			t.Fatalf("%s: allocated %d bytes / %d mallocs for %d bytes of members, want < 1 MiB / 40,000", name, bytesAlloc, mallocs, len(source))
		}
		assertLinearScan(t, name, result, len(source))
		if result.State.WorkspaceMembers != 0 || len(result.Rows) != 1 {
			t.Fatalf("%s: a members list past the cap must not be trusted: state %+v rows %d", name, result.State, len(result.Rows))
		}
	}
}

// TestParseUVLockMembersCapBoundary pins the members cap: a list of exactly
// uvLockMaxMembers names is trusted, one more name is not.
func TestParseUVLockMembersCapBoundary(t *testing.T) {
	t.Parallel()

	entry := "\n[[package]]\nname = \"m-1\"\nversion = \"1.0\"\nsource = { editable = \"m\" }\n"
	for _, n := range []int{uvLockMaxMembers, uvLockMaxMembers + 1} {
		src := "version = 1\n[manifest]\nmembers = [" + distinctMembers(n, ", ") + "]\n" + entry
		result := parseUVLockBytes([]byte(src))
		trusted := n <= uvLockMaxMembers
		if (result.State.WorkspaceMembers == 1) != trusted || (len(result.Rows) == 0) != trusted {
			t.Fatalf("%d listed names: state %+v rows %d, want trusted=%v", n, result.State, len(result.Rows), trusted)
		}
	}
}
