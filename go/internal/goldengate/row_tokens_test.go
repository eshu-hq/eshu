// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package goldengate

import (
	"strings"
	"testing"
)

// TestEvaluateUnresolvedRowTokensSeeded is the seeded RED/GREEN pair for the
// #6782 graph-level junk-token check. NornicDB v1.3.3 stores the literal text
// "row.<key>" when an UNWIND row map omits a key its statement reads; the
// check must fail on that shape and pass a graph without it.
func TestEvaluateUnresolvedRowTokensSeeded(t *testing.T) {
	t.Parallel()

	clean := []GraphElementProperties{
		{Kind: "edge", Name: "DEPENDS_ON", Properties: map[string]any{"source_tool": "helm", "evidence_source": "resolver/cross-repo"}},
		// A nil (Neo4j absent / NornicDB null-valued) property is correct.
		{Kind: "edge", Name: "DEPENDS_ON", Properties: map[string]any{"source_tool": nil}},
		// Dotted values that are not an unresolved row reference.
		{Kind: "node", Name: "File", Properties: map[string]any{"path": "row.go", "name": "row.txt", "ref": "rows.a", "alias": "values.row"}},
		{Kind: "node", Name: "Function", Properties: map[string]any{"line": int64(4), "tags": []any{"a", "b"}}},
	}
	if f := EvaluateUnresolvedRowTokens(clean); !f.OK || !f.Required {
		t.Fatalf("clean graph: finding = %+v, want OK and Required", f)
	}

	seeded := append(append([]GraphElementProperties{}, clean...),
		GraphElementProperties{Kind: "edge", Name: "DEPENDS_ON", Properties: map[string]any{"source_tool": "row.source_tool"}},
		GraphElementProperties{Kind: "edge", Name: "CALLS", Properties: map[string]any{"call_kind": "row.call_kind"}},
		GraphElementProperties{Kind: "edge", Name: "CALLS", Properties: map[string]any{"call_kind": "row.call_kind"}},
		GraphElementProperties{Kind: "node", Name: "EvidenceArtifact", Properties: map[string]any{"refs": []any{"v1", "row.ref_value"}}},
		// The key-equals-property form, with no underscore in the key.
		GraphElementProperties{Kind: "node", Name: "Workload", Properties: map[string]any{"name": "row.name"}},
		// A key that differs from the property and has no underscore (#6782
		// F-6): the S3/EC2 posture writers and the registry name writers read
		// row.state, row.reason, row.version under another property name.
		GraphElementProperties{Kind: "node", Name: "CloudResource", Properties: map[string]any{"s3_internet_exposure_state": "row.state"}},
		GraphElementProperties{Kind: "node", Name: "PackageVersion", Properties: map[string]any{"name": "row.version"}},
	)
	f := EvaluateUnresolvedRowTokens(seeded)
	if f.OK {
		t.Fatalf("seeded graph: finding = %+v, want a failure", f)
	}
	for _, want := range []string{
		"7 element properties",
		"edge CALLS.call_kind=row.call_kind (2)",
		"edge DEPENDS_ON.source_tool=row.source_tool (1)",
		"node EvidenceArtifact.refs=row.ref_value (1)",
		"node Workload.name=row.name (1)",
		"node CloudResource.s3_internet_exposure_state=row.state (1)",
		"node PackageVersion.name=row.version (1)",
	} {
		if !strings.Contains(f.Detail, want) {
			t.Fatalf("seeded detail missing %q:\n%s", want, f.Detail)
		}
	}
}
