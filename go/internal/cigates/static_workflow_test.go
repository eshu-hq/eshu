// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryStaticContractWorkflowRunsSerialProductContracts(t *testing.T) {
	t.Parallel()

	path := filepath.Join(repositoryRoot(t), ".github", "workflows", "static-contract-gates.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read static contract workflow: %v", err)
	}
	root, err := workflowRoot(raw)
	if err != nil {
		t.Fatalf("parse static contract workflow: %v", err)
	}
	jobs, err := mappingValue(root, "jobs")
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs.Content)/2 != 1 {
		t.Fatalf("static-contract-gates.yml jobs = %d, want 1 single serial job", len(jobs.Content)/2)
	}
	gate, err := mappingValue(jobs, "gate")
	if err != nil {
		t.Fatal(err)
	}
	name, err := mappingValue(gate, "name")
	if err != nil || name.Value != "Product Contract Gates" {
		t.Fatalf("gate job name = %v (err=%v), want Product Contract Gates", name, err)
	}
	timeout, err := mappingValue(gate, "timeout-minutes")
	if err != nil || strings.TrimSpace(timeout.Value) != "15" {
		t.Fatalf("gate job timeout-minutes = %v (err=%v), want 15", timeout, err)
	}
	content := string(raw)
	for _, verifier := range []string{
		"scripts/verify-openapi.sh",
		"scripts/verify-edge-source-tool-coverage.sh",
		"scripts/verify-evidence-continuity.sh",
		"scripts/generate-contracttest.sh",
		"scripts/generate-fact-kind-registry.sh",
		"scripts/verify-parser-relationship-kit.sh",
		"scripts/verify-telemetry-coverage.sh",
		"scripts/generate-operator-dashboard.sh",
		"scripts/verify-env-registry-doc.sh",
		"scripts/verify-cassette-author.sh",
	} {
		if !strings.Contains(content, verifier) {
			t.Errorf("static-contract-gates.yml missing product contract verifier %q", verifier)
		}
	}
}
