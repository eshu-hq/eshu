// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestIACRuntimeNativeHandoff(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"main.tf":               "terraform {\n required_version = \">= 1.6.0\"\n}\n",
		"terragrunt.hcl":        "terraform_version_constraint = \"~> 1.6\"\n",
		".terragrunt-version":   "0.58.0\n",
		"charts/app/Chart.yaml": "apiVersion: v2\nname: app\nkubeVersion: '>= 1.27.0'\n",
	}
	for path, body := range files {
		writeCollectorTestFile(t, filepath.Join(root, path), body)
	}
	observed := time.Date(2026, time.October, 29, 0, 0, 0, 0, time.UTC)
	snapshot, err := (NativeRepositorySnapshotter{Now: func() time.Time { return observed }}).SnapshotRepository(context.Background(), SelectedRepository{RepoPath: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.IACRuntime) != 3 {
		t.Fatalf("IAC handoffs = %d, want 3", len(snapshot.IACRuntime))
	}
	for _, payload := range snapshot.FileData {
		if _, ok := payload["iac_runtime_evidence"]; ok {
			t.Fatal("private IAC evidence persisted")
		}
	}
	collected := buildStreamingGeneration(root, testCollectorRepositoryMetadata(root), "run-iac", observed, snapshot, false, "")
	declarations := decodeDeclarationList(t, drainFactChannel(collected.Facts))
	if len(declarations) != 4 {
		t.Fatalf("runtime declarations = %+v", declarations)
	}
	kinds := map[string]bool{}
	for _, row := range declarations {
		kinds[row.SourceKind] = true
		if row.LineNumber < 1 {
			t.Fatalf("missing line: %+v", row)
		}
	}
	for _, kind := range []string{"terraform_required_version", "terragrunt_version_constraint", "terragrunt_version", "helm_chart_kube_version"} {
		if !kinds[kind] {
			t.Fatalf("missing %q in %+v", kind, declarations)
		}
	}
}
