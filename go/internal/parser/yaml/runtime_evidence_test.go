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
