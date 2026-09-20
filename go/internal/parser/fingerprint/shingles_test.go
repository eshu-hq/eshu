// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fingerprint

import (
	"sort"
	"testing"

	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

// shingleFixture stays above MinTokenCount: FingerprintBody emits no
// renamed/sketch/shingle output below the floor, so a small fixture could
// not pin the emission contract this test exists to cover.
const shingleFixture = `package maps

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	c := len(out) + 1
	d := c * 2 - 3
	e := d + c + 5
	f := e * d - 7
	g := f + e + 9
	h := g * f - 11
	i := h + g + 13
	j := i * h - 15
	k := j + i + 17
	m := k * j - 19
	n := m + k + 21
	return out
}
`

// TestAttachEmitsShingleSet pins the #6837 emission contract: full-tier
// bodies carry the renamed 5-shingle set (sorted unique FNV hashes, hex)
// so the reducer can verify exact Jaccard without reading source.
func TestAttachEmitsShingleSet(t *testing.T) {
	t.Parallel()

	root, src := parseOne(t, tree_sitter_go.Language, shingleFixture)
	body := firstFuncBody(t, root, "function_declaration")
	item := map[string]any{}
	stats := &Stats{}
	if reason := Attach("go", false, body, src, item, stats); reason != "" {
		t.Fatalf("Attach() reason = %q, want attached", reason)
	}
	enc, ok := item[KeyShingles].(string)
	if !ok || enc == "" {
		t.Fatalf("item[%q] missing, want hex shingle set", KeyShingles)
	}
	set, err := DecodeShingles(enc)
	if err != nil {
		t.Fatalf("DecodeShingles() error = %v", err)
	}
	if len(set) == 0 {
		t.Fatal("shingle set is empty")
	}
	if !sort.SliceIsSorted(set, func(i, j int) bool { return set[i] < set[j] }) {
		t.Fatal("shingle set must persist sorted for merge-join Jaccard")
	}
	seen := map[uint64]bool{}
	for _, h := range set {
		if seen[h] {
			t.Fatalf("duplicate shingle hash %x: set must be unique", h)
		}
		seen[h] = true
	}
	// Determinism across attaches of the same body.
	item2 := map[string]any{}
	Attach("go", false, body, src, item2, &Stats{})
	if item2[KeyShingles] != item[KeyShingles] {
		t.Fatal("shingle encoding is not deterministic")
	}
}

// TestAttachOmitsShinglesBelowFloor pins the negative leg: sub-floor and
// exact-only bodies carry no shingle set.
func TestAttachOmitsShinglesBelowFloor(t *testing.T) {
	t.Parallel()

	root, src := parseOne(t, tree_sitter_go.Language, "package tiny\n\nfunc one() int { return 1 }\n")
	body := firstFuncBody(t, root, "function_declaration")
	item := map[string]any{}
	Attach("go", false, body, src, item, &Stats{})
	if _, ok := item[KeyShingles]; ok {
		t.Fatal("sub-floor body must not carry shingles")
	}
}
