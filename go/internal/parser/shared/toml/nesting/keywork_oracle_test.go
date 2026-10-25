// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nesting

import (
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// tableDocGenerator builds valid TOML that piles entries into a few parent
// tables through every route the guard counts: plain and dotted keys, inline
// tables, child headers, and array-of-tables sections. Names never repeat, so
// go-toml accepts every document, and strings carry `=`, `[`, and `#` so a
// lexer slip that counts string content shows up as a miscount.
type tableDocGenerator struct {
	r    *rand.Rand
	next int
	b    strings.Builder
}

func (g *tableDocGenerator) name() string {
	g.next++
	return fmt.Sprintf("n%d", g.next)
}

// value is a scalar, array, or inline table, each a legal value position.
func (g *tableDocGenerator) value(depth int) string {
	switch pick := g.r.IntN(6); {
	case pick == 0:
		return `"v = [{#}]"`
	case pick == 1:
		return "[1, 2, 3]"
	case pick == 2 && depth < 3:
		var parts []string
		for range g.r.IntN(5) {
			parts = append(parts, g.name()+" = "+g.value(depth+1))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case pick == 3 && depth < 3:
		return "[" + g.value(depth+1) + ", " + g.value(depth+1) + "]"
	default:
		return "1"
	}
}

// scope writes one table's body: plain keys, dotted keys into a shared child.
func (g *tableDocGenerator) scope() {
	dotted := g.name()
	for range g.r.IntN(40) {
		switch g.r.IntN(3) {
		case 0:
			fmt.Fprintf(&g.b, "%s = %s\n", g.name(), g.value(0))
		case 1:
			fmt.Fprintf(&g.b, "%s.%s = %s\n", dotted, g.name(), g.value(0))
		default:
			fmt.Fprintf(&g.b, "%s = %s # c = 1\n", g.name(), g.value(0))
		}
	}
}

func (g *tableDocGenerator) document() []byte {
	g.b.Reset()
	g.next = 0
	g.scope()
	parents := []string{"deps", "dev", "build"}
	for _, p := range parents {
		if g.r.IntN(2) == 0 {
			fmt.Fprintf(&g.b, "[%s]\n", p)
			g.scope()
		}
	}
	for range g.r.IntN(60) {
		switch g.r.IntN(4) {
		case 0:
			fmt.Fprintf(&g.b, "[[arr]]\n")
		case 1:
			fmt.Fprintf(&g.b, "[[ %s.list ]]\n", parents[g.r.IntN(len(parents))])
		default:
			// A child of a parent that may also hold keys of its own.
			fmt.Fprintf(&g.b, "[%s.%s]\n", parents[g.r.IntN(len(parents))], g.name())
		}
		g.scope()
	}
	return []byte(g.b.String())
}

// largestTable is the entry count of the biggest table in a decoded value,
// the number the decoder's key lookup pays the square of.
func largestTable(v any) int {
	switch t := v.(type) {
	case map[string]any:
		most := len(t)
		for _, child := range t {
			most = max(most, largestTable(child))
		}
		return most
	case []any:
		most := 0
		for _, child := range t {
			most = max(most, largestTable(child))
		}
		return most
	default:
		return 0
	}
}

// TestKeyWorkNeverUndercountsTheDecoder is the oracle for the guard's
// soundness: for every document go-toml accepts, twice the guard's work is at
// least the square of the largest table go-toml builds. A form the guard
// stops counting makes a table outgrow the bound, and this test fails.
func TestKeyWorkNeverUndercountsTheDecoder(t *testing.T) {
	g := &tableDocGenerator{r: rand.New(rand.NewPCG(8991, 20261025))}
	var biggest int
	for i := range generatorCases(t) {
		doc := g.document()
		var decoded map[string]any
		if err := toml.Unmarshal(doc, &decoded); err != nil {
			t.Fatalf("case %d: go-toml rejected the generated document: %v\n%s", i, err, doc)
		}
		got, err := scanBounds(doc, math.MaxInt, math.MaxInt)
		if err != nil {
			t.Fatalf("case %d: scanBounds() = %v", i, err)
		}
		table := largestTable(decoded)
		biggest = max(biggest, table)
		if 2*got.work < table*table {
			t.Fatalf("case %d: work %d is under half of largest table %d squared\n%s", i, got.work, table, doc)
		}
	}
	if biggest < 30 {
		t.Fatalf("generator built tables of at most %d entries, want some of at least 30", biggest)
	}
}
