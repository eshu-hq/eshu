// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fingerprint

import (
	"strings"
	"testing"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

func parseGoBody(t *testing.T, src string) (*tree_sitter.Node, *tree_sitter.Node, []byte) {
	t.Helper()
	p := tree_sitter.NewParser()
	t.Cleanup(func() { p.Close() })
	if err := p.SetLanguage(tree_sitter.NewLanguage(tree_sitter_go.Language())); err != nil {
		t.Fatalf("SetLanguage: %v", err)
	}
	bs := []byte(src)
	tree := p.Parse(bs, nil)
	if tree == nil {
		t.Fatal("parse returned nil tree")
	}
	t.Cleanup(func() { tree.Close() })
	root := tree.RootNode()
	var found *tree_sitter.Node
	var visit func(n *tree_sitter.Node)
	visit = func(n *tree_sitter.Node) {
		if found != nil {
			return
		}
		if n.Kind() == "function_declaration" {
			found = n
			return
		}
		for i := range n.ChildCount() {
			visit(n.Child(i))
		}
	}
	visit(root)
	if found == nil {
		t.Fatal("no function_declaration found")
	}
	return root, found.ChildByFieldName("body"), bs
}

const bigGoFunc = `package p

func process(items []int, limit int) int {
	total := 0
	count := 0
	for _, value := range items {
		if value < 0 {
			continue
		}
		if count >= limit {
			break
		}
		total = total + value*2
		count = count + 1
	}
	average := 0
	if count > 0 {
		average = total / count
	}
	return total + average + count
}
`

func TestAttachAboveFloorSetsAllKeys(t *testing.T) {
	_, body, src := parseGoBody(t, bigGoFunc)
	item := map[string]any{}
	stats := &Stats{}
	if reason := Attach("go", false, body, src, item, stats); reason != "" {
		t.Fatalf("Attach skipped with reason %q", reason)
	}
	for _, key := range []string{KeyExact, KeyRenamed, KeySketch, KeyTokenCount} {
		if _, ok := item[key]; !ok {
			t.Fatalf("expected item key %q to be set", key)
		}
	}
	if count, ok := item[KeyTokenCount].(int); !ok || count < MinTokenCount {
		t.Fatalf("expected token count >= %d, got %v", MinTokenCount, item[KeyTokenCount])
	}
	if stats.Fingerprinted != 1 {
		t.Fatalf("expected 1 fingerprinted, got %+v", stats)
	}
	// Determinism: same input, same keys.
	again := map[string]any{}
	if reason := Attach("go", false, body, src, again, &Stats{}); reason != "" {
		t.Fatalf("second Attach skipped with reason %q", reason)
	}
	for _, key := range []string{KeyExact, KeyRenamed, KeySketch, KeyTokenCount} {
		if item[key] != again[key] {
			t.Fatalf("key %q not deterministic: %v vs %v", key, item[key], again[key])
		}
	}
}

func TestAttachBelowFloorSkips(t *testing.T) {
	_, body, src := parseGoBody(t, "package p\nfunc f() int {\n\treturn 1\n}\n")
	item := map[string]any{"name": "f"}
	stats := &Stats{}
	if reason := Attach("go", false, body, src, item, stats); reason != ReasonBelowFloor {
		t.Fatalf("expected %q, got %q", ReasonBelowFloor, reason)
	}
	if len(item) != 1 {
		t.Fatalf("below-floor attach must set no keys, got %v", item)
	}
	if stats.BelowFloor != 1 {
		t.Fatalf("expected 1 below-floor skip, got %+v", stats)
	}
}

func TestAttachHasErrorSkips(t *testing.T) {
	_, body, src := parseGoBody(t, bigGoFunc)
	item := map[string]any{}
	stats := &Stats{}
	if reason := Attach("go", true, body, src, item, stats); reason != ReasonHasError {
		t.Fatalf("expected %q, got %q", ReasonHasError, reason)
	}
	if len(item) != 0 {
		t.Fatalf("has-error attach must set no keys, got %v", item)
	}
	if stats.HasErrorSkipped != 1 {
		t.Fatalf("expected 1 has-error skip, got %+v", stats)
	}
}

func TestAttachNoBodySkips(t *testing.T) {
	stats := &Stats{}
	if reason := Attach("go", false, nil, []byte("x"), map[string]any{}, stats); reason != ReasonNoBody {
		t.Fatalf("expected %q, got %q", ReasonNoBody, reason)
	}
	if stats.NoBody != 1 {
		t.Fatalf("expected 1 no-body skip, got %+v", stats)
	}
}

func TestAttachNilStatsIsSafe(t *testing.T) {
	_, body, src := parseGoBody(t, bigGoFunc)
	item := map[string]any{}
	if reason := Attach("go", false, body, src, item, nil); reason != "" {
		t.Fatalf("Attach with nil stats skipped: %q", reason)
	}
}

func TestSketchRoundTripAndBands(t *testing.T) {
	_, body, src := parseGoBody(t, bigGoFunc)
	res := FingerprintBody("go", body, src)
	enc := EncodeSketch(res.Sketch)
	dec, err := DecodeSketch(enc)
	if err != nil {
		t.Fatal(err)
	}
	if len(dec) != SketchRegs {
		t.Fatalf("expected %d registers, got %d", SketchRegs, len(dec))
	}
	for i := range dec {
		if dec[i] != res.Sketch[i] {
			t.Fatalf("register %d mismatch after round trip", i)
		}
	}
	bands := BandHashes(res.Sketch)
	if len(bands) != LSHBands {
		t.Fatalf("expected %d bands, got %d", LSHBands, len(bands))
	}
	if _, err := DecodeSketch("bogus"); err == nil {
		t.Fatal("expected error decoding bogus sketch")
	}
}

// TestBandHashesRejectsShortSketch proves a well-formed but short sketch
// (valid register multiple, wrong register count, e.g. hand-crafted entity
// metadata) cannot panic the band derivation the #6837 reducer reuses.
func TestBandHashesRejectsShortSketch(t *testing.T) {
	for _, sketch := range [][]uint64{nil, {}, {1}, make([]uint64, SketchRegs-1)} {
		if got := BandHashes(sketch); len(got) != 0 {
			t.Fatalf("BandHashes(%d registers) = %d bands, want none", len(sketch), len(got))
		}
	}
}

func TestRenameSemanticsOnBigBody(t *testing.T) {
	mkSrc := func(extra string) string {
		return `package p

func process(items []int, limit int) int {
	total := 0
	count := 0
	for _, value := range items {
		if value < 0 {
			continue
		}
		if count >= limit {
			break
		}
		total = total + value*2
		count = count + 1
	}
	average := 0
	if count > 0 {
		average = total / count
	}
` + extra + `}
`
	}
	mk := func(src string) map[string]any {
		_, body, bs := parseGoBody(t, src)
		item := map[string]any{}
		if reason := Attach("go", false, body, bs, item, &Stats{}); reason != "" {
			t.Fatalf("Attach skipped: %q", reason)
		}
		return item
	}
	baseSrc := mkSrc("\treturn total + average + count\n")
	base := mk(baseSrc)
	commentOnly := mk(mkSrc("\t// total with average and count\n\treturn total + average + count\n"))
	if base[KeyExact] != commentOnly[KeyExact] {
		t.Fatal("comment-only edit must leave exact unchanged")
	}
	// A pure rename replaces every occurrence, so positional ids are stable.
	renamed := mk(strings.ReplaceAll(baseSrc, "count", "tally"))
	if base[KeyExact] == renamed[KeyExact] {
		t.Fatal("pure rename must change exact")
	}
	if base[KeyRenamed] != renamed[KeyRenamed] {
		t.Fatal("pure rename must leave renamed unchanged")
	}
	edited := mk(mkSrc("\treturn total + average + count + 1\n"))
	if base[KeyExact] == edited[KeyExact] || base[KeyRenamed] == edited[KeyRenamed] {
		t.Fatal("one-statement edit must change both fingerprints")
	}
	if sim := sketchSimilarity(base[KeySketch].(string), edited[KeySketch].(string), t); sim < 0.7 {
		t.Fatalf("one-statement edit must keep sketch similarity high, got %f", sim)
	}
}

func sketchSimilarity(a, b string, t *testing.T) float64 {
	t.Helper()
	da, err := DecodeSketch(a)
	if err != nil {
		t.Fatal(err)
	}
	db, err := DecodeSketch(b)
	if err != nil {
		t.Fatal(err)
	}
	same := 0
	for i := range da {
		if da[i] == db[i] {
			same++
		}
	}
	return float64(same) / float64(len(da))
}
