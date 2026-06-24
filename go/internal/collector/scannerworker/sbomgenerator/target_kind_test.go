// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbomgenerator

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestAnalyzerGeneratesFactsForImageAndArtifactTargets(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		kind scannerworker.TargetKind
	}{
		{name: "image", kind: scannerworker.TargetImage},
		{name: "artifact", kind: scannerworker.TargetArtifact},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			input := testClaimInput(t)
			input.Target.Kind = tc.kind
			input.Target.ScopeID = "scanner-worker://" + string(tc.kind) + "/target-private-name"
			source := &stubSource{
				inventory: Inventory{
					SubjectDigest: "sha256:11111111111111111111111111111111111111111111111111111111111111aa",
					Components: []Component{
						{PURL: "pkg:npm/foo@1.2.3", Name: "foo", Version: "1.2.3"},
					},
				},
			}
			analyzer := Analyzer{Source: source, Now: testClock}

			result, err := analyzer.Analyze(context.Background(), input)
			if err != nil {
				t.Fatalf("Analyze(%q) error = %v, want nil", tc.kind, err)
			}
			if counts := countFactKinds(result.Output.Facts); counts[facts.SBOMDocumentFactKind] != 1 || counts[facts.SBOMComponentFactKind] != 1 {
				t.Fatalf("fact counts = %v, want one document and one component", counts)
			}
			if err := scannerworker.ValidateFactOutput(input, result.Output); err != nil {
				t.Fatalf("ValidateFactOutput(%q) error = %v, want nil", tc.kind, err)
			}
		})
	}
}
