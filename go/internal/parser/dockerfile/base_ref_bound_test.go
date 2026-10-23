// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dockerfile

import (
	"strings"
	"testing"
	"time"
)

// hostileNestedFrom builds a FROM line of about size bytes made of balanced,
// nested ${A:-word} blocks. Expanding it by recursive descent rescans the rest
// of the line at every level.
func hostileNestedFrom(size int) string {
	levels := size / len("${A:-}")
	return "FROM x:" + strings.Repeat("${A:-", levels) + "v" + strings.Repeat("}", levels)
}

func TestBaseRefOversizedNestedReferenceIsUnresolvedAndFast(t *testing.T) {
	t.Parallel()

	source := hostileNestedFrom(60_000) + "\n"
	start := time.Now()
	got := RuntimeMetadata(source)
	elapsed := time.Since(start)

	if elapsed > 250*time.Millisecond {
		t.Fatalf("one 60 KB nested reference took %v, want well under 250ms", elapsed)
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
	t.Parallel()

	var source strings.Builder
	for i := 0; i < 200; i++ {
		source.WriteString(hostileNestedFrom(60_000))
		source.WriteString("\n")
	}
	start := time.Now()
	got := RuntimeMetadata(source.String())
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Fatalf("200 hostile FROM lines took %v, want a linear-time bound under 2s", elapsed)
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
