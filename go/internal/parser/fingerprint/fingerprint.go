// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fingerprint

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"sort"
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
	// Shingles holds the sorted unique FNV-64a shingle identities the
	// #6837 reducer verifies exact Jaccard over. Nil for exact-only tiers.
	Shingles []uint64
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

// productionLangAliases maps parser-emitted language keys that differ from
// the grammar shorthand to the registry key. C# parsing emits "c_sharp"
// while the grammar (and both tables below) register "csharp"; without the
// alias production C# falls through to unknown exact-only with no comment
// exclusion, so a comment-only edit would change fp_exact.
var productionLangAliases = map[string]string{
	"c_sharp": "csharp",
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

func shingleHash(sh string, reg uint64) uint64 {
	h := fnv.New64a()
	// hash.Hash.Write never returns an error; the blank assignment records
	// the intentional ignore for the gosec unhandled-error rule.
	_, _ = h.Write([]byte(sh))
	return splitmix64(h.Sum64() + reg*0x9e3779b97f4a7c15)
}

// renamedShingles renders the ShingleK token shingles minHash sketches.
// Short bodies yield one shingle from the available tokens.
func renamedShingles(toks []string) []string {
	shingles := []string{}
	if len(toks) < ShingleK {
		shingles = append(shingles, strings.Join(toks, "\x02"))
		return shingles
	}
	for i := 0; i+ShingleK <= len(toks); i++ {
		shingles = append(shingles, strings.Join(toks[i:i+ShingleK], "\x02"))
	}
	return shingles
}

// shingleIDs renders the sorted unique FNV-64a identities of the renamed
// shingles: the exact set the #6837 reducer verifies Jaccard over.
// Sorted for merge-join intersection without re-sorting per pair.
func shingleIDs(toks []string) []uint64 {
	seen := map[uint64]bool{}
	ids := []uint64{}
	for _, s := range renamedShingles(toks) {
		h := fnv.New64a()
		_, _ = h.Write([]byte(s))
		if id := h.Sum64(); !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// minHash computes SketchRegs registers over ShingleK shingles with
// stdlib-only mixing. Deterministic for identical input.
func minHash(toks []string) []uint64 {
	shingles := renamedShingles(toks)
	sketch := make([]uint64, SketchRegs)
	for r := uint64(0); r < SketchRegs; r++ {
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
			binary.LittleEndian.PutUint64(buf[:], sketch[b*LSHRows+r])
			_, _ = h.Write(buf[:])
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
//
// The exact hash and token count are always computed: they are cheap and
// Attach needs the count for the emission floor. Renamed hashing, MinHash,
// and band derivation run only for full-tier bodies at or above
// MinTokenCount — Attach discards sub-floor results, so generating them
// would be pure waste at corpus scale. FingerprintBody never fails, so it
// returns just the result.
func FingerprintBody(lang string, body *tree_sitter.Node, src []byte) *Result {
	return FingerprintBodies(lang, []*tree_sitter.Node{body}, src)
}

// FingerprintBodies fingerprints the concatenated leaf stream of several
// body nodes in order (issue #6865: every defining equation of a Haskell
// function feeds one fingerprint). A single body hashes exactly as
// FingerprintBody: both funnel into this one core, so single-equation output
// is byte-identical. Nil bodies are skipped; an all-nil or empty list yields
// the zero-leaf result the caller maps to its below-floor skip.
func FingerprintBodies(lang string, bodies []*tree_sitter.Node, src []byte) *Result {
	normalized := strings.ToLower(strings.TrimSpace(lang))
	if alias, ok := productionLangAliases[normalized]; ok {
		normalized = alias
	}
	tab, full := tables[normalized]
	comments := exactOnlyComments[normalized]
	if full {
		comments = tab.comments
	}
	if comments == nil {
		comments = map[string]bool{}
	}
	var leaves []leaf
	for _, body := range bodies {
		if body == nil {
			continue
		}
		collectLeaves(body, src, comments, &leaves)
	}
	exact, renamed := classify(leaves, tab.literals)
	res := &Result{
		Exact:      hashTokens(exact),
		TokenCount: len(leaves),
	}
	if !full || len(leaves) < MinTokenCount {
		return res
	}
	res.RenamedSupported = true
	res.Renamed = hashTokens(renamed)
	res.Sketch = minHash(renamed)
	res.Bands = bands(res.Sketch)
	res.Shingles = shingleIDs(renamed)
	return res
}
