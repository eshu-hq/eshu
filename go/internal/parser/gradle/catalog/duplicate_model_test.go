// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// dupKeys is a small key pool, so the generator repeats keys often. A quoted
// key is the same TOML key as its bare form.
var dupKeys = []string{"x", "y", `"x"`, "x-y"}

// dupSubKeys are the second and third segments of dotted keys and headers.
var dupSubKeys = []string{"module", "version", "ref", "id"}

// dupKey draws a key of one to three segments.
func dupKey(rng *rand.Rand) string {
	parts := []string{dupKeys[rng.Intn(len(dupKeys))]}
	for n := rng.Intn(3); n > 0; n-- {
		parts = append(parts, dupSubKeys[rng.Intn(len(dupSubKeys))])
	}
	return strings.Join(parts, ".")
}

// dupValue draws a value: a string, a scalar from scalarCases (legal or
// not), an array, or an inline table whose keys may repeat or extend one
// another.
func dupValue(rng *rand.Rand, depth int) string {
	switch rng.Intn(6) {
	case 0:
		return `"g:a:1.0"`
	case 1:
		return `["x", "y"]`
	case 2:
		return scalarCases[rng.Intn(len(scalarCases))]
	case 3:
		return "[" + scalarCases[rng.Intn(len(scalarCases))] + ", " + scalarCases[rng.Intn(len(scalarCases))] + "]"
	default:
		if depth > 2 {
			return `"1.0"`
		}
		var pairs []string
		n := 1 + rng.Intn(3)
		if depth == 0 && rng.Intn(4) == 0 {
			// Up to 24 segments: often past maxInlineKeys, so the index
			// checks the table instead of the list scan.
			n = 6 + rng.Intn(3)
		}
		for ; n > 0; n-- {
			pairs = append(pairs, dupKey(rng)+" = "+dupValue(rng, depth+1))
		}
		return "{ " + strings.Join(pairs, ", ") + " }"
	}
}

// dupDocument writes a catalog whose keys repeat or redefine one another
// across the dotted, header, array-table and inline forms, in the four
// catalog tables, an unsupported table and the root.
func dupDocument(rng *rand.Rand) string {
	tables := []string{"versions", "libraries", "plugins", "bundles", "metadata"}
	var b strings.Builder
	for n := rng.Intn(2); n > 0; n-- {
		fmt.Fprintf(&b, "%s = %s\n", dupKey(rng), dupValue(rng, 0))
	}
	for n := 1 + rng.Intn(5); n > 0; n-- {
		table := tables[rng.Intn(len(tables))]
		switch rng.Intn(4) {
		case 0:
			fmt.Fprintf(&b, "[%s.%s]\n", table, dupKey(rng))
		case 1:
			fmt.Fprintf(&b, "[[%s.%s]]\n", table, dupKey(rng))
		default:
			fmt.Fprintf(&b, "[%s]\n", table)
		}
		for k := rng.Intn(4); k > 0; k-- {
			fmt.Fprintf(&b, "%s = %s\n", dupKey(rng), dupValue(rng, 0))
		}
	}
	return b.String()
}

// Every generated catalog is valid TOML syntax, and most repeat or
// redefine a key or hold a value the decoder rejects. Scan must reject exactly the files go-toml's full decode
// rejects, with no rows, and parse the rest.
func TestScanRejectsExactlyTheKeyRedefinitionsTOMLRejects(t *testing.T) {
	cases := modelCases(t)
	rng := rand.New(rand.NewSource(88222))
	var rejected, accepted int
	for doc := 0; doc < cases; doc++ {
		text := dupDocument(rng)
		result := Scan([]byte(text))
		if tomlRejects(text) {
			rejected++
			if result.State != StateMalformed || len(result.Rows) != 0 {
				t.Fatalf("doc %d: State=%q rows=%d, want malformed with none (TOML rejects it)\n%s",
					doc, result.State, len(result.Rows), text)
			}
			continue
		}
		accepted++
		if result.State != StateParsed {
			t.Fatalf("doc %d: State=%q (%s), want parsed (TOML accepts it)\n%s", doc, result.State, result.ParseError, text)
		}
	}
	t.Logf("cases=%d rejected by TOML=%d accepted=%d", cases, rejected, accepted)
	if rejected < cases/10 || accepted < cases/10 {
		t.Fatalf("generator is lopsided: %d rejected, %d accepted of %d", rejected, accepted, cases)
	}
}
