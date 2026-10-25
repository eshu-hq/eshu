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

// tableDocGenerator builds valid TOML that piles lookups onto a few parent
// tables through every route the guard charges: plain and multi-segment
// dotted keys, inline tables, child headers of several segments, headers
// that come before their parent, and array-of-tables sections that repeat.
// Names never repeat except for array tables, so go-toml accepts every
// document, and strings carry `=`, `[`, and `#` so a lexer slip that charges
// string content shows up as a miscount.
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
			parts = append(parts, g.dottedKey(g.r.IntN(2))+" = "+g.value(depth+1))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case pick == 3 && depth < 3:
		return "[" + g.value(depth+1) + ", " + g.value(depth+1) + "]"
	default:
		return "1"
	}
}

// dottedKey returns a key of extra+1 fresh segments.
func (g *tableDocGenerator) dottedKey(extra int) string {
	parts := make([]string, 0, extra+1)
	for range extra + 1 {
		parts = append(parts, g.name())
	}
	return strings.Join(parts, ".")
}

// scope writes one table's body. A scope is usually short and now and then a
// burst, because the cost the guard bounds is the quadratic one.
func (g *tableDocGenerator) scope() {
	count := g.r.IntN(12)
	if g.r.IntN(6) == 0 {
		count = g.r.IntN(150)
	}
	for range count {
		switch g.r.IntN(4) {
		case 0:
			fmt.Fprintf(&g.b, "%s = %s\n", g.name(), g.value(0))
		case 1:
			fmt.Fprintf(&g.b, "%s = %s\n", g.dottedKey(1+g.r.IntN(3)), g.value(0))
		case 2:
			fmt.Fprintf(&g.b, "\"%s\" = %s # c = 1\n", g.name(), g.value(0))
		default:
			fmt.Fprintf(&g.b, "%s = %s\n", g.name(), g.value(0))
		}
	}
}

func (g *tableDocGenerator) document() []byte {
	g.b.Reset()
	g.next = 0
	g.scope()
	parents := []string{"deps", "dev", "build"}
	defined := map[string]bool{}
	for range g.r.IntN(120) {
		p := parents[g.r.IntN(len(parents))]
		switch g.r.IntN(7) {
		case 0, 1:
			g.b.WriteString("[[arr]]\n")
		case 2:
			fmt.Fprintf(&g.b, "[[ %s.list ]]\n", p)
		case 3:
			// A child of a parent that may be defined before or after it.
			fmt.Fprintf(&g.b, "[%s.%s]\n", p, g.name())
		case 4:
			fmt.Fprintf(&g.b, "[%s.%s.%s]\n", p, g.name(), g.name())
		case 5:
			fmt.Fprintf(&g.b, "[%s]\n", g.name())
		default:
			if defined[p] {
				continue
			}
			defined[p] = true
			fmt.Fprintf(&g.b, "[%s]\n", p)
		}
		g.scope()
	}
	return []byte(g.b.String())
}

// TestKeyWorkNeverUndercountsTheDecoder is the oracle for the guard's
// soundness. A step-counting copy of go-toml's key tracker replays every
// document go-toml accepts, and the guard's work must be at least the steps
// the copy takes. A lookup the guard stops charging makes the steps outgrow
// the work, and this test fails.
func TestKeyWorkNeverUndercountsTheDecoder(t *testing.T) {
	g := &tableDocGenerator{r: rand.New(rand.NewPCG(8991, 20261025))}
	var mostSteps int
	for i := range generatorCases(t) {
		doc := g.document()
		var decoded map[string]any
		if err := toml.Unmarshal(doc, &decoded); err != nil {
			t.Fatalf("case %d: go-toml rejected the generated document: %v\n%s", i, err, doc)
		}
		steps, err := trackerSteps(doc, nil)
		if err != nil {
			t.Fatalf("case %d: step tracker rejected a document go-toml accepts: %v\n%s", i, err, doc)
		}
		got, err := scanBounds(doc, Bounds{Depth: math.MaxInt, KeyWork: math.MaxInt})
		if err != nil {
			t.Fatalf("case %d: scanBounds() = %v", i, err)
		}
		mostSteps = max(mostSteps, steps)
		if got.work < steps {
			t.Fatalf("case %d: work %d is under the %d lookup steps go-toml takes\n%s", i, got.work, steps, doc)
		}
	}
	if mostSteps < 5000 {
		t.Fatalf("generator took at most %d steps, want some documents of at least 5000", mostSteps)
	}
}

// TestKeyWorkUpperBoundsTheReviewShapes runs the hostile layouts from the
// review through the step-counting tracker at a size small enough to test, and
// requires the work to cover the steps and to grow with the repeat count.
func TestKeyWorkUpperBoundsTheReviewShapes(t *testing.T) {
	var lastWork int
	for _, repeats := range []int{200, 400, 800} {
		for name, doc := range repeatedHeaderDocs(300, repeats) {
			steps, err := trackerSteps([]byte(doc), nil)
			if err != nil {
				t.Fatalf("%s: step tracker: %v", name, err)
			}
			got, err := scanBounds([]byte(doc), Bounds{Depth: math.MaxInt, KeyWork: math.MaxInt})
			if err != nil {
				t.Fatalf("%s: scanBounds() = %v", name, err)
			}
			if got.work < steps {
				t.Errorf("%s x%d: work %d is under the %d steps go-toml takes", name, repeats, got.work, steps)
			}
			if steps < 300*repeats/2 {
				t.Errorf("%s x%d: only %d steps, the shape no longer repeats the sibling scan", name, repeats, steps)
			}
			lastWork = got.work
		}
	}
	if lastWork == 0 {
		t.Fatal("no work measured")
	}
}

// TestStructShapeWorkCoversTheDecoder replays lockfile-shaped documents through
// the tracker the way a struct decode walks them: plain entries after the
// first key the struct does not know are never looked up. The guard's work
// under StructFields must still cover the steps.
func TestStructShapeWorkCoversTheDecoder(t *testing.T) {
	shape := &decodeShape{
		root:   map[string]bool{"package": true},
		tables: map[string]map[string]bool{"package": {"name": true, "version": true, "source": true, "dependencies": true}},
	}
	for name, doc := range map[string][]byte{
		"v1 lockfile":        legalLockfileV1(300),
		"v3 lockfile":        legalLockfile(300),
		"unknown first key":  []byte("[[package]]\nx = 1\nname = \"a\"\n[metadata]\na = 1\nb = 2\n"),
		"root keys":          []byte("version = 3\nother = 1\nmore = 2\n[[package]]\nname = \"a\"\n"),
		"inline first entry": []byte("[metadata]\nx = {a=1, b=2, c=3, d=4, e=5, f=6}\n"),
		"many unknown":       []byte("[[package]]\nx = 1\n" + strings.Repeat("k = 1\n", 0) + "[foo]\na=1\n[[package]]\nname=\"b\"\n"),
	} {
		t.Run(name, func(t *testing.T) {
			steps, err := trackerSteps(doc, shape)
			if err != nil {
				t.Fatalf("step tracker: %v", err)
			}
			got, err := scanBounds(doc, Bounds{Depth: math.MaxInt, KeyWork: math.MaxInt, StructFields: 4})
			if err != nil {
				t.Fatalf("scanBounds() = %v", err)
			}
			if got.work < steps {
				t.Fatalf("work %d is under the %d steps a struct decode takes", got.work, steps)
			}
		})
	}
}
