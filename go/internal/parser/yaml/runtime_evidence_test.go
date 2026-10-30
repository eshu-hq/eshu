// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestHelmChartKubeVersionEvidence(t *testing.T) {
	file := filepath.Join(t.TempDir(), "Chart.yaml")
	if err := os.WriteFile(file, []byte("apiVersion: v2\nname: example\nkubeVersion: '>= 1.27.0'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	payload, err := Parse(file, false, shared.Options{})
	if err != nil {
		t.Fatal(err)
	}
	evidence, ok := payload["iac_runtime_evidence"].(shared.RuntimeEvidence)
	if !ok || len(evidence.Values) != 1 || evidence.Values[0].SourceKind != "helm_chart_kube_version" || evidence.Values[0].Line != 3 {
		t.Fatalf("runtime evidence = %#v", payload["iac_runtime_evidence"])
	}
}

func TestStackResolverEvidence(t *testing.T) {
	tests := []struct {
		name         string
		filename     string
		body         string
		want         []shared.RuntimeValue
		wantRejected int
		wantState    string
		wantKey      bool
		wantErr      bool
	}{
		{"resolver", "stack.yaml", "packages:\n  - .\nresolver: lts-22.43\nextra-deps: []\n", []shared.RuntimeValue{{SourceKind: "stack_yaml_resolver", Runtime: "haskell", Value: "lts-22.43", Line: 3}}, 0, "parsed_values", true, false},
		{"absent", "stack.yaml", "packages:\n  - .\n", nil, 0, "parsed_empty", true, false},
		{"empty", "stack.yaml", "", nil, 0, "parsed_empty", true, false},
		{"malformed", "stack.yaml", "resolver: [unclosed\n", nil, 0, "", false, true},
		{"mapping", "stack.yaml", "resolver:\n  snapshot: custom\n", nil, 1, "parsed_empty", true, false},
		{"nested", "stack.yaml", "packages:\n  - resolver: lts-22.43\n", nil, 0, "parsed_empty", true, false},
		{"credential", "stack.yaml", "resolver: https://user:password@example.com/lts\n", nil, 1, "parsed_empty", true, false},
		{"lock", "stack.yaml.lock", "resolver: lts-22.43\n", nil, 0, "", false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), tc.filename)
			if err := os.WriteFile(file, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			payload, err := Parse(file, false, shared.Options{})
			if tc.wantErr {
				if err == nil {
					t.Fatal("Parse() succeeded, want decode error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			evidence, ok := payload["haskell_runtime_evidence"].(shared.RuntimeEvidence)
			if ok != tc.wantKey {
				t.Fatalf("haskell_runtime_evidence present = %v, want %v", ok, tc.wantKey)
			}
			if !tc.wantKey {
				return
			}
			if len(evidence.Values) != len(tc.want) || evidence.Rejected != tc.wantRejected || evidence.State != tc.wantState || evidence.Digest != shared.RuntimeDigest([]byte(tc.body)) {
				t.Fatalf("evidence = %+v", evidence)
			}
			for i, want := range tc.want {
				if evidence.Values[i] != want {
					t.Fatalf("value = %+v, want %+v", evidence.Values[i], want)
				}
			}
		})
	}
}

func TestStackYamlCloudFormationFallsThrough(t *testing.T) {
	body := "AWSTemplateFormatVersion: '2010-09-09'\nResources:\n  Logs:\n    Type: AWS::S3::Bucket\n"
	file := filepath.Join(t.TempDir(), "stack.yaml")
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	payload, err := Parse(file, false, shared.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["haskell_runtime_evidence"]; ok {
		t.Fatal("CloudFormation stack.yaml claimed as Stack evidence")
	}
	resources := payload["cloudformation_resources"].([]map[string]any)
	if len(resources) != 1 || resources[0]["name"] != "Logs" {
		t.Fatalf("cloudformation_resources = %+v", resources)
	}
}

func TestHelmChartIgnoresNestedKubeVersion(t *testing.T) {
	file := filepath.Join(t.TempDir(), "Chart.yaml")
	if err := os.WriteFile(file, []byte("apiVersion: v2\nname: example\nannotations:\n  kubeVersion: '1.27.0'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	payload, err := Parse(file, false, shared.Options{})
	if err != nil {
		t.Fatal(err)
	}
	evidence := payload["iac_runtime_evidence"].(shared.RuntimeEvidence)
	if len(evidence.Values) != 0 {
		t.Fatalf("nested value emitted %+v", evidence.Values)
	}
}
