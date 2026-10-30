// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"testing"

	runtimedeclaration "github.com/eshu-hq/eshu/go/internal/collector/repo/runtime/declaration"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// iacDeclaredExtraction is one Terraform required_version declaration, the
// shape collectIACRuntimeSnapshot binds for an admitted main.tf.
func iacDeclaredExtraction(t *testing.T) runtimedeclaration.Extraction {
	t.Helper()
	extraction := runtimedeclaration.ExtractIACEvidence(shared.RuntimeEvidence{
		Values: []shared.RuntimeValue{
			{SourceKind: "terraform_required_version", Runtime: "terraform", Value: ">= 1.6.0", Line: 2},
		},
		ByteSize: 64,
	})
	if extraction.Len() == 0 {
		t.Fatal("test setup: IaC extraction holds no declaration")
	}
	return extraction
}

// TestNoteParsedIACCandidateAdmitsOnlyAdmittedIACPaths proves the IaC lane
// refuses what every sibling lane refuses: an IaC file under a dependency
// directory (node_modules, vendor, bower_components, jspm_packages), even when
// a preserved glob brings it back, and a non-admitted path record no
// candidate, while the four admitted IaC sources still record one. A
// dependency's own version field is never this repository's declaration.
func TestNoteParsedIACCandidateAdmitsOnlyAdmittedIACPaths(t *testing.T) {
	t.Parallel()

	declared := iacDeclaredExtraction(t)
	inputs := map[string]runtimedeclaration.Extraction{
		"declared": declared,
		"skipped":  runtimedeclaration.UnavailableIACExtraction(128),
	}
	for _, path := range []string{
		"vendor/charts/app/Chart.yaml",
		"node_modules/terraform/main.tf",
		"bower_components/infra/terragrunt.hcl",
		"jspm_packages/charts/app/Chart.yml",
		"docs/notes.txt",
		"infra/main.tf.bak",
	} {
		for name, extraction := range inputs {
			candidates := map[string]Candidate{}
			NoteParsedIACCandidate(candidates, path, extraction)
			if len(candidates) != 0 {
				t.Errorf("NoteParsedIACCandidate(%q, %s) recorded %d candidates, want 0", path, name, len(candidates))
			}
		}
	}

	for _, path := range []string{
		"main.tf",
		"infra/main.tf",
		"terragrunt.hcl",
		"env/prod/terragrunt.hcl",
		"charts/app/Chart.yaml",
		"charts/app/Chart.yml",
	} {
		candidates := map[string]Candidate{}
		NoteParsedIACCandidate(candidates, path, declared)
		if len(candidates) != 1 {
			t.Errorf("NoteParsedIACCandidate(%q, declared) recorded %d candidates, want 1", path, len(candidates))
		}
	}
}
