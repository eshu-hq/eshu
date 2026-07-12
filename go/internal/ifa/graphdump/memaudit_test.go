// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphdump

import (
	"context"
	"fmt"
	"testing"
)

// synthProps builds a realistic node property map (~8 keys, ~30-char values),
// modeling a Function/File/CloudResource node the reducer materializes.
func synthProps(i int) map[string]any {
	return map[string]any{
		"name":            fmt.Sprintf("symbol_name_%08d_pkg_module", i),
		"kind":            "function",
		"file_path":       fmt.Sprintf("go/internal/pkg%04d/file_%06d.go", i%1000, i),
		"scope_id":        fmt.Sprintf("acme-demo-gcp-%02d", i%50),
		"generation_id":   fmt.Sprintf("generation-%016x", i),
		"source_fact_id":  fmt.Sprintf("fact-%024x", i),
		"stable_fact_key": fmt.Sprintf("stable-key-%020d", i),
		"signature":       fmt.Sprintf("func(ctx context.Context, a int%d) (T%d, error)", i%7, i%11),
	}
}

// genReader is a streaming Reader that GENERATES nodes/edges on the fly in the
// yield callback, never holding a slice — modeling the production boltGraphReader
// streaming from the Bolt result cursor. It is the correct fixture for measuring
// the #5009 streaming win: a fakeReader would hold the whole struct set in its
// slice regardless of Canonicalize's streaming.
type genReader struct{ nodeCount, edgeCount int }

func (g genReader) StreamNodes(_ context.Context, yield func(Node) error) error {
	for i := 0; i < g.nodeCount; i++ {
		if err := yield(Node{Labels: []string{"Function", "Symbol"}, Props: synthProps(i)}); err != nil {
			return err
		}
	}
	return nil
}

func (g genReader) StreamEdges(_ context.Context, yield func(Edge) error) error {
	for i := 0; i < g.edgeCount; i++ {
		from := i % g.nodeCount
		to := (i * 7) % g.nodeCount
		if err := yield(Edge{
			Type: "CALLS", FromLabels: []string{"Function", "Symbol"}, FromProps: synthProps(from),
			ToLabels: []string{"Function", "Symbol"}, ToProps: synthProps(to),
			Props: map[string]any{"call_site_line": i % 5000, "confidence": "high"},
		}); err != nil {
			return err
		}
	}
	return nil
}

// The heavy peak-heap measurement (TestMemAuditCanonicalizeScale) lives in
// scale_memaudit_test.go behind the `ifamemaudit` build tag so it never runs in
// an ordinary CI lane; genReader and synthProps below are its fixtures and are
// also the fixtures for the always-on byte-identity regression here.

// goldenDigests pins Canonicalize's exact output digest for deterministic
// synthetic graphs of several shapes. It is the byte-identity regression for
// the #5009 streaming + direct-assembly rewrite: any change to the canonical
// output (record shape, sort, indentation, framing) breaks these, which is
// exactly what must NOT drift, since graphdump is the determinism matrix's
// comparison basis. The digests were captured from the pre-rewrite
// in-memory-decode implementation.
var goldenDigests = []struct {
	nodes, edges int
	digest       string
}{
	{0, 0, "8657af7032f150516a7289bb7774205e2eecad747573c5404fd3c01107f18fe0"},
	{1, 0, "87d3fe9b43fa0b9303df194ec45a0db9072b82292791afa732a10503923b12bf"},
	{10, 20, "933eb6c4249f8905f3933c203d089ff65d99506163685ba10745811f7dbeecaa"},
	{1000, 4000, "75c5f5e569c277391984355a6de01fa58e0090c00bda69e5dbbd772a3d3b4754"},
	{5000, 20000, "a64285622914119f539dd23b6f73c6bef7160925bc70f4a44b9e168eb111010e"},
}

func TestCanonicalizeGoldenDigests(t *testing.T) {
	t.Parallel()
	for _, g := range goldenDigests {
		var r Reader = genReader{nodeCount: g.nodes, edgeCount: g.edges}
		got, err := Digest(context.Background(), r)
		if err != nil {
			t.Fatalf("nodes=%d edges=%d: Digest error: %v", g.nodes, g.edges, err)
		}
		if got != g.digest {
			t.Errorf("nodes=%d edges=%d: digest = %s, want %s (canonical output drifted — graphdump is the determinism matrix basis)",
				g.nodes, g.edges, got, g.digest)
		}
	}
}
