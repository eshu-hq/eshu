// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hcl

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestRuntimeEvidenceFromTerraformAndTerragrunt(t *testing.T) {
	for _, tc := range []struct{ name, source, kind, value string }{
		{"main.tf", "terraform {\n  required_version = \">= 1.6.0\"\n}\n", "terraform_required_version", ">= 1.6.0"},
		{"terragrunt.hcl", "terraform_version_constraint = \"~> 1.6\"\n", "terragrunt_version_constraint", "~> 1.6"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), tc.name)
			if err := os.WriteFile(file, []byte(tc.source), 0o600); err != nil {
				t.Fatal(err)
			}
			payload, err := Parse(file, false, shared.Options{})
			if err != nil {
				t.Fatal(err)
			}
			evidence, ok := payload["iac_runtime_evidence"].(shared.RuntimeEvidence)
			if !ok || len(evidence.Values) != 1 || evidence.Values[0].SourceKind != tc.kind || evidence.Values[0].Value != tc.value {
				t.Fatalf("runtime evidence = %#v", payload["iac_runtime_evidence"])
			}
		})
	}
}

func TestRuntimeEvidenceIgnoresDynamicAndNestedValues(t *testing.T) {
	for _, tc := range []struct{ name, source string }{
		{"main.tf", "variable \"version\" {}\nterraform { required_version = var.version }\nresource \"x\" \"y\" { required_version = \"1.6\" }\n"},
		{"terragrunt.hcl", "terraform_version_constraint = local.required_version\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), tc.name)
			if err := os.WriteFile(file, []byte(tc.source), 0o600); err != nil {
				t.Fatal(err)
			}
			payload, err := Parse(file, false, shared.Options{})
			if err != nil {
				t.Fatal(err)
			}
			evidence := payload["iac_runtime_evidence"].(shared.RuntimeEvidence)
			if len(evidence.Values) != 0 {
				t.Fatalf("dynamic or nested values emitted %+v", evidence.Values)
			}
		})
	}
}
