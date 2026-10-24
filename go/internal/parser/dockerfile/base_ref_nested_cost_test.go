// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dockerfile

import (
	"runtime"
	"strings"
	"testing"
)

// nestedPaddedFromLine builds one FROM line whose reference stays inside both
// the reference-length cap and the nesting-depth cap, yet writes a large known
// value at every nesting level: ${U:-${K}${U:-${K}...x}}. U is never declared,
// so each level takes its word, and K is a value just under the output cap.
func nestedPaddedFromLine(depth int) string {
	return "FROM img:" + strings.Repeat("${U:-${K}", depth) + "x" + strings.Repeat("}", depth)
}

// nestedPaddedSource is an ARG whose default is 4000 bytes followed by lines
// FROM lines of nestedPaddedFromLine.
func nestedPaddedSource(lines, depth int) string {
	var source strings.Builder
	source.WriteString("ARG K=" + strings.Repeat("k", 4000) + "\n")
	line := nestedPaddedFromLine(depth) + "\n"
	for i := 0; i < lines; i++ {
		source.WriteString(line)
	}
	return source.String()
}

// allocatedBytes returns the heap bytes fn allocates. The callers are not
// parallel: runtime.MemStats is process-wide, so a concurrent test would
// pollute the delta. Allocation is the bound these tests pin because wall time
// depends on the host, the race detector and the other jobs on the runner.
func allocatedBytes(fn func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// TestBaseRefNestedPaddedReferenceAllocationIsBounded pins the allocation cost
// of a hostile but in-bounds reference. Each level of the recursion used to own
// a builder and copy its whole result outward, so the bytes allocated grew with
// depth squared times the value size and a 645 KB file cost gigabytes. It reads
// allocation, not wall time, because wall time depends on the host.
func TestBaseRefNestedPaddedReferenceAllocationIsBounded(t *testing.T) {
	// Not parallel: runtime.MemStats is process-wide, so a concurrent test
	// would pollute the delta.
	const allocCeiling = 50 << 20
	source := nestedPaddedSource(1000, 63)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	got := RuntimeMetadata(source)
	runtime.ReadMemStats(&after)

	allocated := after.TotalAlloc - before.TotalAlloc
	if allocated > allocCeiling {
		t.Fatalf("%d bytes of hostile FROM lines allocated %d bytes, want under %d",
			len(source), allocated, allocCeiling)
	}
	if len(got.Stages) != 1000 {
		t.Fatalf("stages = %d, want 1000", len(got.Stages))
	}
	stage := stageAtLine(t, got, 2)
	if len(stage.UnresolvedArgs) == 0 {
		t.Fatalf("UnresolvedArgs is empty, want the over-cap expansion marked unresolved")
	}
	if !strings.HasPrefix(stage.BaseTag, "${U:-") {
		t.Fatalf("BaseTag = %.40q, want the placeholder kept as written", stage.BaseTag)
	}
}

// BenchmarkBaseRefNestedPadded measures the hostile in-bounds nested shape at
// the 1000-line size the review measured.
func BenchmarkBaseRefNestedPadded(b *testing.B) {
	source := nestedPaddedSource(1000, 63)
	b.ReportAllocs()
	b.SetBytes(int64(len(source)))
	for i := 0; i < b.N; i++ {
		RuntimeMetadata(source)
	}
}
