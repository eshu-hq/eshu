// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nesting

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// generatorCasesEnv raises the seeded case count for a local deep run, for
// example ESHU_NESTING_GENERATOR_CASES=30000.
const generatorCasesEnv = "ESHU_NESTING_GENERATOR_CASES"

// defaultGeneratorCases keeps the in-package run quick.
const defaultGeneratorCases = 4000

// docGenerator builds valid TOML whose depth it knows. Every key is unique, so
// go-toml never rejects a document for a redefinition, and every string form
// carries brackets, braces, dots, quotes, and '#' so a lexer that misreads a
// string boundary miscounts.
type docGenerator struct {
	r    *rand.Rand
	next int
	nl   string
	b    strings.Builder
}

func (g *docGenerator) unique() int {
	g.next++
	return g.next
}

// noise is string content that must never count toward depth.
var noise = []string{"[", "]", "{", "}", "#", ".", ",", "=", " ", "x", "[[{{", "a.b.c"}

// bodyText returns content for a string. quote is the delimiter, escapes
// says whether backslash escapes are allowed, and multiline allows newlines.
// The raw delimiter never appears, so the body cannot close the string early.
func (g *docGenerator) bodyText(quote byte, escapes, multiline bool) string {
	var b strings.Builder
	for range g.r.IntN(6) {
		switch pick := g.r.IntN(10); {
		case pick == 0 && escapes:
			b.WriteString(`\"`)
		case pick == 1 && escapes:
			b.WriteString(`\\`)
		case pick == 2 && multiline:
			b.WriteString(g.nl)
		case pick == 3:
			// The other quote character is plain content.
			if quote == '"' {
				b.WriteByte('\'')
			} else {
				b.WriteByte('"')
			}
		default:
			b.WriteString(noise[g.r.IntN(len(noise))])
		}
	}
	return b.String()
}

// stringValue returns one of the four TOML string forms. Multi-line forms may
// end with one or two extra quotes of content before the closing delimiter.
func (g *docGenerator) stringValue(allowNewlines bool) string {
	switch g.r.IntN(4) {
	case 0:
		return `"` + g.bodyText('"', true, false) + `"`
	case 1:
		return "'" + g.bodyText('\'', false, false) + "'"
	case 2:
		extra := strings.Repeat(`"`, g.r.IntN(3))
		return `"""` + g.bodyText('"', true, allowNewlines) + extra + `"""`
	default:
		extra := strings.Repeat("'", g.r.IntN(3))
		return "'''" + g.bodyText('\'', false, allowNewlines) + extra + "'''"
	}
}

// key returns a dotted key with unique segments and the number of dots in it.
func (g *docGenerator) key() (string, int) {
	segments := 1 + g.r.IntN(3)
	if g.r.IntN(8) == 0 {
		segments += g.r.IntN(6)
	}
	parts := make([]string, segments)
	for i := range parts {
		id := g.unique()
		switch g.r.IntN(4) {
		case 0:
			parts[i] = fmt.Sprintf(`"q.[{#'%d"`, id)
		case 1:
			parts[i] = fmt.Sprintf(`'l.]}#"%d'`, id)
		default:
			parts[i] = "k" + strconv.Itoa(id)
		}
	}
	sep := "."
	if g.r.IntN(4) == 0 {
		sep = " . "
	}
	return strings.Join(parts, sep), segments - 1
}

// value writes a value whose container sits at depth and returns the deepest
// depth inside it. inline is true inside an inline table, where newlines are
// not allowed.
func (g *docGenerator) value(depth, budget int, inline bool) (string, int) {
	kind := g.r.IntN(10)
	if budget <= 0 {
		kind = 9
	}
	switch {
	case kind < 3:
		return g.array(depth, budget, inline)
	case kind < 5:
		return g.inlineTable(depth, budget)
	case kind < 7:
		return g.stringValue(!inline), depth
	default:
		scalars := []string{"1", "-7", "1.5", "true", "1979-05-27T07:32:00.999Z", "inf", "0x1F"}
		return scalars[g.r.IntN(len(scalars))], depth
	}
}

func (g *docGenerator) array(depth, budget int, inline bool) (string, int) {
	inner := depth + 1
	deepest := inner
	multiline := !inline && g.r.IntN(2) == 0
	var b strings.Builder
	b.WriteByte('[')
	if multiline && g.r.IntN(2) == 0 {
		b.WriteString(" # ]]] [[[ {{ \"'" + g.nl)
	}
	count := g.r.IntN(4)
	for i := range count {
		text, d := g.value(inner, budget-1, inline)
		deepest = max(deepest, d)
		b.WriteString(text)
		if i < count-1 || g.r.IntN(2) == 0 {
			b.WriteByte(',')
		}
		if multiline {
			if g.r.IntN(3) == 0 {
				b.WriteString(" # [[ {{ . \"")
			}
			b.WriteString(g.nl)
		} else {
			b.WriteByte(' ')
		}
	}
	b.WriteByte(']')
	return b.String(), deepest
}

func (g *docGenerator) inlineTable(depth, budget int) (string, int) {
	inner := depth + 1
	deepest := inner
	var b strings.Builder
	b.WriteString("{ ")
	count := g.r.IntN(4)
	for i := range count {
		key, dots := g.key()
		deepest = max(deepest, inner+dots)
		text, d := g.value(inner+dots, budget-1, true)
		deepest = max(deepest, d)
		b.WriteString(key + " = " + text)
		if i < count-1 {
			b.WriteString(", ")
		}
	}
	b.WriteString(" }")
	return b.String(), deepest
}

// document returns a valid TOML document and its depth.
func (g *docGenerator) document() (string, int) {
	g.b.Reset()
	g.nl = "\n"
	if g.r.IntN(4) == 0 {
		g.nl = "\r\n"
	}
	budget := g.r.IntN(5)
	if g.r.IntN(6) == 0 {
		budget += g.r.IntN(10)
	}
	deepest, base := 0, 0
	for range 1 + g.r.IntN(6) {
		switch g.r.IntN(6) {
		case 0:
			g.b.WriteString("# [[ {{ . \" ' ]]" + g.nl)
		case 1:
			key, dots := g.key()
			if g.r.IntN(2) == 0 {
				g.b.WriteString("[" + key + "]")
			} else {
				g.b.WriteString("[[" + key + "]]")
			}
			if g.r.IntN(3) == 0 {
				g.b.WriteString(" # [[[ {{")
			}
			g.b.WriteString(g.nl)
			base = dots
			deepest = max(deepest, base)
		default:
			key, dots := g.key()
			deepest = max(deepest, base+dots)
			text, d := g.value(base+dots, budget, false)
			deepest = max(deepest, d)
			g.b.WriteString(key + " = " + text + g.nl)
		}
	}
	return g.b.String(), deepest
}

func generatorCases(t *testing.T) int {
	t.Helper()
	raw := os.Getenv(generatorCasesEnv)
	if raw == "" {
		return defaultGeneratorCases
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		t.Fatalf("%s=%q, want a positive integer", generatorCasesEnv, raw)
	}
	return n
}

// TestScanAgreesWithGeneratorOracle checks the guard against documents of
// known depth over every string form, comment placement, header shape, and
// line ending. go-toml must accept each document, so the oracle never rests
// on invalid TOML, and Check must pass at the depth and fail one level below.
func TestScanAgreesWithGeneratorOracle(t *testing.T) {
	t.Parallel()
	g := &docGenerator{r: rand.New(rand.NewPCG(8985, 20261024))}
	cases := generatorCases(t)
	deepestSeen := 0
	for i := range cases {
		doc, want := g.document()
		deepestSeen = max(deepestSeen, want)
		var decoded map[string]any
		if err := toml.Unmarshal([]byte(doc), &decoded); err != nil {
			t.Fatalf("case %d: generator produced invalid TOML (%v):\n%s", i, err, doc)
		}
		got, err := scan([]byte(doc), math.MaxInt)
		if err != nil || got != want {
			t.Fatalf("case %d: scan() = %d, %v; want %d, nil:\n%s", i, got, err, want, doc)
		}
		if err := Check([]byte(doc), want); err != nil {
			t.Fatalf("case %d: Check(limit=%d) = %v, want nil:\n%s", i, want, err, doc)
		}
		if want > 0 {
			if err := Check([]byte(doc), want-1); !errors.Is(err, ErrTooDeep) {
				t.Fatalf("case %d: Check(limit=%d) = %v, want ErrTooDeep:\n%s", i, want-1, err, doc)
			}
		}
	}
	if deepestSeen < 8 {
		t.Fatalf("generator reached depth %d over %d cases, want at least 8", deepestSeen, cases)
	}
	t.Logf("checked %d generated documents, deepest %d", cases, deepestSeen)
}

// FuzzCheck asserts the guard never panics and that every input it accepts
// at a small limit decodes or fails in go-toml without a crash. Run it with
// go test -fuzz=FuzzCheck for a deeper search.
func FuzzCheck(f *testing.F) {
	for _, tc := range depthCases {
		f.Add([]byte(tc.doc))
	}
	f.Add([]byte(`a = ["""x"""", [[1]]]`))
	f.Add([]byte("[a.b]\nc.d = [{e.f = 1}]\n"))
	f.Fuzz(func(t *testing.T, doc []byte) {
		first := Check(doc, 16)
		if second := Check(doc, 16); (first == nil) != (second == nil) {
			t.Fatalf("Check is not deterministic: %v then %v", first, second)
		}
		if first == nil {
			var decoded map[string]any
			_ = toml.Unmarshal(doc, &decoded)
		}
	})
}
