// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// Tunables fixed by the #6834 theory proof. Do not change without
// re-running the precision study and updating the evidence doc.
const (
	// SketchRegs is the MinHash register count.
	SketchRegs = 128
	// ShingleK is the token shingle width for the sketch.
	ShingleK = 5
	// LSHBands is the LSH band count; LSHRows is rows per band.
	LSHBands = 32
	LSHRows  = 4
)

// Result is the fingerprint of one function body node.
type Result struct {
	// Exact is the sha256 hex of the kind+text leaf stream, comments excluded.
	Exact string
	// Renamed is the sha256 hex with identifiers/literals positionally
	// renamed. Empty when RenamedSupported is false.
	Renamed string
	// RenamedSupported is false for exact-only tiers.
	RenamedSupported bool
	// TokenCount is the leaf count of the walked body.
	TokenCount int
	// Sketch holds SketchRegs MinHash registers. Nil for exact-only tiers.
	Sketch []uint64
	// Bands holds LSHBands hex band hashes. Empty for exact-only tiers.
	Bands []string
}

// table holds the per-language leaf classification sets.
type table struct {
	comments map[string]bool
	literals map[string]bool
}

func tsLiterals() map[string]bool {
	return map[string]bool{
		"string": true, "string_fragment": true, "number": true,
		"template_string": true, "template_substitution": true,
		"true": true, "false": true, "null": true, "undefined": true,
		"regex": true,
	}
}

// tables maps parser language names to full-tier classification tables.
// Languages absent here are exact-only: exact hash plus token count, with
// the comment kinds below still excluded from the exact stream.
var tables = map[string]table{
	"go": {
		comments: map[string]bool{"comment": true},
		literals: map[string]bool{
			"int_literal": true, "float_literal": true, "imaginary_literal": true,
			"rune_literal": true, "raw_string_literal": true, "interpreted_string_literal": true,
			"interpreted_string_literal_content": true, "raw_string_literal_content": true,
			"escape_sequence": true,
			"true":            true, "false": true, "nil": true,
		},
	},
	"python": {
		comments: map[string]bool{"comment": true},
		literals: map[string]bool{
			"string": true, "string_content": true, "integer": true, "float": true,
			"true": true, "false": true, "none": true,
		},
	},
	"typescript": {comments: map[string]bool{"comment": true}, literals: tsLiterals()},
	"tsx":        {comments: map[string]bool{"comment": true}, literals: tsLiterals()},
	"javascript": {comments: map[string]bool{"comment": true}, literals: tsLiterals()},
	"java": {
		comments: map[string]bool{"line_comment": true, "block_comment": true},
		literals: map[string]bool{
			"string_literal": true, "string_fragment": true,
			"decimal_integer_literal": true, "hex_integer_literal": true,
			"octal_integer_literal": true, "binary_integer_literal": true,
			"decimal_floating_point_literal": true, "hex_floating_point_literal": true,
			"character_literal": true, "boolean_literal": true, "null_literal": true,
			"true": true, "false": true,
		},
	},
}

// exactOnlyComments maps every wired exact-only language to its grammar's
// comment node kinds. Comment sets were determined empirically per grammar
// (parse a comment-bearing function, collect kinds containing "comment") and
// are pinned by TestExactOnlyTiersIgnoreComments: a comment-only edit must
// not change fp_exact on any of these tiers. Perl POD parses as a top-level
// `pod` node, never inside a function body, so it needs no entry.
var exactOnlyComments = map[string]map[string]bool{
	"c":       {"comment": true},
	"cpp":     {"comment": true},
	"csharp":  {"comment": true},
	"dart":    {"comment": true},
	"elixir":  {"comment": true},
	"groovy":  {"line_comment": true, "block_comment": true},
	"haskell": {"comment": true},
	"kotlin":  {"line_comment": true, "block_comment": true},
	"perl":    {"comment": true},
	"php":     {"comment": true},
	"ruby":    {"comment": true},
	"rust":    {"line_comment": true, "block_comment": true},
	"scala":   {"comment": true, "block_comment": true},
	"swift":   {"comment": true, "multiline_comment": true},
}

type leaf struct {
	kind string
	text string
}

func collectLeaves(node *tree_sitter.Node, src []byte, comments map[string]bool, out *[]leaf) {
	kind := node.Kind()
	if comments[kind] {
		return
	}
	n := node.ChildCount()
	if n == 0 {
		if t := node.Utf8Text(src); t != "" {
			*out = append(*out, leaf{kind: kind, text: t})
		}
		return
	}
	for i := range n {
		collectLeaves(node.Child(i), src, comments, out)
	}
}

func isIdent(kind string) bool { return strings.Contains(strings.ToLower(kind), "identifier") }

func classify(leaves []leaf, literals map[string]bool) (exact, renamed []string) {
	exact = make([]string, 0, len(leaves))
	renamed = make([]string, 0, len(leaves))
	idMap := map[string]int{}
	litMap := map[string]int{}
	for _, l := range leaves {
		exact = append(exact, l.kind+"\x01"+l.text)
		switch {
		case isIdent(l.kind):
			id, ok := idMap[l.text]
			if !ok {
				id = len(idMap)
				idMap[l.text] = id
			}
			renamed = append(renamed, fmt.Sprintf("I%d", id))
		case literals[l.kind]:
			id, ok := litMap[l.text]
			if !ok {
				id = len(litMap)
				litMap[l.text] = id
			}
			renamed = append(renamed, fmt.Sprintf("L%d", id))
		default:
			renamed = append(renamed, l.kind+"\x01"+l.text)
		}
	}
	return exact, renamed
}

func hashTokens(toks []string) string {
	h := sha256.New()
	for _, t := range toks {
		h.Write([]byte(t))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func splitmix64(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

func shingleHash(sh string, reg int) uint64 {
	h := fnv.New64a()
	h.Write([]byte(sh))
	return splitmix64(h.Sum64() + uint64(reg)*0x9e3779b97f4a7c15)
}

// minHash computes SketchRegs registers over ShingleK shingles with
// stdlib-only mixing. Deterministic for identical input.
func minHash(toks []string) []uint64 {
	shingles := []string{}
	if len(toks) < ShingleK {
		shingles = append(shingles, strings.Join(toks, "\x02"))
	} else {
		for i := 0; i+ShingleK <= len(toks); i++ {
			shingles = append(shingles, strings.Join(toks[i:i+ShingleK], "\x02"))
		}
	}
	sketch := make([]uint64, SketchRegs)
	for r := 0; r < SketchRegs; r++ {
		best := ^uint64(0)
		for _, s := range shingles {
			if v := shingleHash(s, r); v < best {
				best = v
			}
		}
		sketch[r] = best
	}
	return sketch
}

// bands hashes the sketch into LSHBands hex band keys.
func bands(sketch []uint64) []string {
	out := make([]string, 0, LSHBands)
	for b := 0; b < LSHBands; b++ {
		h := fnv.New64a()
		for r := 0; r < LSHRows; r++ {
			var buf [8]byte
			v := sketch[b*LSHRows+r]
			for i := 0; i < 8; i++ {
				buf[i] = byte(v >> (8 * i))
			}
			h.Write(buf[:])
		}
		out = append(out, fmt.Sprintf("%016x", h.Sum64()))
	}
	return out
}

// FingerprintBody fingerprints one function body node. Whitespace never
// appears as tree-sitter leaves, so no whitespace normalization applies.
// Comments are excluded from the exact stream on every known tier, full or
// exact-only. Unknown languages are exact-only with no comment exclusion:
// their comment kinds are not known, so their leaves stay in the stream.
func FingerprintBody(lang string, body *tree_sitter.Node, src []byte) (*Result, error) {
	normalized := strings.ToLower(strings.TrimSpace(lang))
	tab, full := tables[normalized]
	comments := exactOnlyComments[normalized]
	if full {
		comments = tab.comments
	}
	if comments == nil {
		comments = map[string]bool{}
	}
	var leaves []leaf
	collectLeaves(body, src, comments, &leaves)
	exact, renamed := classify(leaves, tab.literals)
	res := &Result{
		Exact:      hashTokens(exact),
		TokenCount: len(leaves),
	}
	if !full {
		return res, nil
	}
	res.RenamedSupported = true
	res.Renamed = hashTokens(renamed)
	res.Sketch = minHash(renamed)
	res.Bands = bands(res.Sketch)
	return res, nil
}
