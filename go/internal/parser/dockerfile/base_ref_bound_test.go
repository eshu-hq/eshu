// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dockerfile

import (
	"strings"
	"testing"
)

// hostileNestedFrom builds a FROM line of about size bytes made of balanced,
// nested ${A:-word} blocks. Expanding it by recursive descent rescans the rest
// of the line at every level.
func hostileNestedFrom(size int) string {
	levels := size / len("${A:-}")
	return "FROM x:" + strings.Repeat("${A:-", levels) + "v" + strings.Repeat("}", levels)
}

func TestBaseRefOversizedNestedReferenceIsUnresolvedAndCheap(t *testing.T) {
	// Not parallel: allocatedBytes reads process-wide MemStats.
	source := hostileNestedFrom(60_000) + "\n"
	var got Metadata
	allocated := allocatedBytes(func() { got = RuntimeMetadata(source) })
	// Measured 544,040 bytes; the unbounded expansion of the same line
	// allocated about 836 MB.
	if allocated > 4<<20 {
		t.Fatalf("one 60 KB nested reference allocated %d bytes, want under 4 MiB", allocated)
	}
	stage := stageAtLine(t, got, 1)
	if len(stage.UnresolvedArgs) == 0 {
		t.Fatalf("UnresolvedArgs is empty, want the oversized reference marked unresolved")
	}
	if !strings.HasPrefix(stage.BaseTag, "${A:-") {
		t.Fatalf("BaseTag = %.40q, want the placeholder kept as written", stage.BaseTag)
	}
}

func TestBaseRefManyNestedReferencesStayBounded(t *testing.T) {
	// Not parallel: allocatedBytes reads process-wide MemStats.
	var source strings.Builder
	for i := 0; i < 200; i++ {
		source.WriteString(hostileNestedFrom(60_000))
		source.WriteString("\n")
	}
	var got Metadata
	allocated := allocatedBytes(func() { got = RuntimeMetadata(source.String()) })
	// Measured 83,665,144 bytes; with the reference bound disabled the same
	// source allocated about 167 GB.
	if allocated > 256<<20 {
		t.Fatalf("200 hostile FROM lines allocated %d bytes, want a linear-cost bound under 256 MiB", allocated)
	}
	if len(got.Stages) != 200 {
		t.Fatalf("stages = %d, want 200", len(got.Stages))
	}
}

func TestBaseRefDeeplyNestedShortReferenceIsUnresolved(t *testing.T) {
	t.Parallel()

	// Closed and short, but nested far deeper than any real Dockerfile.
	depth := maxBraceDepth + 10
	ref := "x:" + strings.Repeat("${A:-", depth) + "v" + strings.Repeat("}", depth)
	got := RuntimeMetadata("FROM " + ref + "\n")
	stage := stageAtLine(t, got, 1)
	if len(stage.UnresolvedArgs) == 0 || stage.BaseTag != ref[len("x:"):] {
		t.Fatalf("deep nesting = %#v, want unresolved with the placeholder kept", stage)
	}
}

func TestBaseRefOrdinaryNestingStillResolves(t *testing.T) {
	t.Parallel()

	got := RuntimeMetadata("ARG V=3.20\nFROM alpine:${MISSING:-${V:-edge}}\n")
	stage := stageAtLine(t, got, 2)
	if stage.BaseTag != "3.20" || len(stage.UnresolvedArgs) != 0 {
		t.Fatalf("nested default = %#v, want alpine:3.20 resolved", stage)
	}
}

// BenchmarkBaseRefExpansion measures a typical FROM against the hostile
// 60 KB nested line and the padded ARG chain so the guard's cost on ordinary
// input stays visible.
func BenchmarkBaseRefExpansion(b *testing.B) {
	ordinary := "ARG NODE_VERSION=24.1.0\nFROM node:${NODE_VERSION}-slim AS app\nRUN true\n"
	hostile := hostileNestedFrom(60_000) + "\n"
	b.Run("ordinary", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			RuntimeMetadata(ordinary)
		}
	})
	padded := paddedArgChain(100, 4000) + strings.Repeat("FROM i:${A100}\n", 1000)
	b.Run("padded_chain_100_args_1000_froms", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			RuntimeMetadata(padded)
		}
	})
	b.Run("hostile_60KB", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			RuntimeMetadata(hostile)
		}
	})
}
