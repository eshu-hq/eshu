// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"testing"

	"github.com/eshu-hq/eshu-sdk/factschema"
	runtimedeclarationv1 "github.com/eshu-hq/eshu-sdk/factschema/runtimedeclaration/v1"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/runtime/declaration"
)

// TestRuntimeDeclarationEntityIDNumericRenderingParity pins that the shared
// five-part derivation renders identically from a real extracted row (where
// line_number and order_index are int, the SDK struct's Go type) and from
// live graph properties (where the same values arrive as int64). The offline
// guard and the assert-edges endpoint rule both call RuntimeDeclarationEntityID,
// so a rendering gap here would make the live assertion compare a different
// identity than the guard proved -- a false mismatch on every edge, or a
// false pass on a wrong one.
//
// The row is built from real extracted-row types: the envelope is encoded
// through factschema.EncodeRuntimeDeclarationVersion and decoded through the
// production declaration.ExtractRows, never hand-built maps standing in for
// the extractor's output.
func TestRuntimeDeclarationEntityIDNumericRenderingParity(t *testing.T) {
	t.Parallel()

	payload, err := factschema.EncodeRuntimeDeclarationVersion(runtimedeclarationv1.Version{
		RepoID:       "repository:eshu-fixture-runtime-pins",
		RelativePath: ".tool-versions",
		ScopeDir:     ".",
		LineNumber:   1,
		OrderIndex:   0,
		SourceKind:   "tool_versions",
		Runtime:      "node",
		RawSpec:      "20.11.1",
		SpecKind:     runtimedeclarationv1.SpecKindExact,
	})
	if err != nil {
		t.Fatalf("encode runtime_declaration.version payload: %v", err)
	}
	rows, quarantined, err := declaration.ExtractRows([]facts.Envelope{{
		FactKind:         facts.RuntimeDeclarationVersionFactKind,
		SchemaVersion:    facts.RuntimeDeclarationSchemaVersionV1,
		CollectorKind:    "git",
		SourceConfidence: facts.SourceConfidenceObserved,
		Payload:          payload,
	}})
	if err != nil {
		t.Fatalf("ExtractRows: %v", err)
	}
	if len(quarantined) != 0 || len(rows) != 1 {
		t.Fatalf("ExtractRows produced %d rows (%d quarantined), want exactly 1 row and none quarantined", len(rows), len(quarantined))
	}
	guardSide := RuntimeDeclarationEntityID(rows[0])

	graphSide := RuntimeDeclarationEntityID(map[string]any{
		"repo_id":       "repository:eshu-fixture-runtime-pins",
		"relative_path": ".tool-versions",
		"line_number":   int64(1),
		"runtime":       "node",
		"order_index":   int64(0),
	})

	const want = "repository:eshu-fixture-runtime-pins|.tool-versions|1|node|0"
	if guardSide != want {
		t.Errorf("RuntimeDeclarationEntityID(extracted row) = %q, want %q; the guard renders a different identity than the fixture pins", guardSide, want)
	}
	if graphSide != want {
		t.Errorf("RuntimeDeclarationEntityID(graph int64 props) = %q, want %q; the endpoint rule renders a different identity than the guard proved", graphSide, want)
	}
}
