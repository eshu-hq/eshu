// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbomgenerator

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// TestComponentIdentityKeepsLookalikesApart pins #9584 for generated SBOMs:
// component identity folds ASCII letters only, so a component spelled with
// U+212A or U+0130 never shares an identity with the real component. A
// Unicode fold made them duplicates, and the analyzer dropped whichever came
// second, so a lookalike listed first erased the real kafka component.
func TestComponentIdentityKeepsLookalikesApart(t *testing.T) {
	t.Parallel()

	for _, pair := range [][2]Component{
		{{PURL: "pkg:npm/\u212Aafka@1.0.0"}, {PURL: "pkg:npm/kafka@1.0.0"}},
		{{Name: "Ser\u0130log", Version: "1.0.0"}, {Name: "Serilog", Version: "1.0.0"}},
		{{Name: "serilog", Version: "1.0.0-\u0130"}, {Name: "serilog", Version: "1.0.0-i"}},
	} {
		if componentIdentity(pair[0]) == componentIdentity(pair[1]) {
			t.Errorf("componentIdentity(%+v) equals the real component's %q", pair[0], componentIdentity(pair[1]))
		}
	}
	if componentIdentity(Component{PURL: "pkg:npm/Kafka@1.0.0"}) != componentIdentity(Component{PURL: "pkg:npm/kafka@1.0.0"}) {
		t.Error("componentIdentity splits ASCII case variants")
	}

	source := &stubSource{inventory: Inventory{
		SubjectDigest: "sha256:11111111111111111111111111111111111111111111111111111111111111aa",
		SpecVersion:   "1.6",
		SourceTool:    "eshu_scanner_sbom_generator 0.0.1",
		Components: []Component{
			{PURL: "pkg:npm/\u212Aafka@1.0.0", Name: "\u212Aafka", Version: "1.0.0", Type: "library"},
			{PURL: "pkg:npm/kafka@1.0.0", Name: "kafka", Version: "1.0.0", Type: "library"},
		},
	}}
	result, err := Analyzer{Source: source, Now: testClock}.Analyze(context.Background(), testClaimInput(t))
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if got := countFactKinds(result.Output.Facts)[chain.SBOMComponentFactKind]; got != 2 {
		t.Fatalf("component facts = %d, want 2 (the lookalike must not displace the real kafka component)", got)
	}
}
