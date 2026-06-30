// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

const minimalValidYAML = `version: v1
gates:
  - id: openapi-surface
    name: Verify OpenAPI Surface
    category: exactness
    tier: pre-pr
    blocking: true
    triggers:
      - "go/internal/query/openapi*.go"
    local:
      command: "bash scripts/verify-openapi.sh"
      test_command: "bash scripts/test-verify-openapi.sh"
    ci:
      workflow: verify-openapi.yml
      job: "Verify OpenAPI gate"
    requirements:
      - go
    ci_only_reason: ""
    local_only_reason: ""
`

func writeYAML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "ci-gates.v1.yaml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoad_ValidRegistry(t *testing.T) {
	t.Parallel()
	path := writeYAML(t, minimalValidYAML)
	reg, err := cigates.Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if reg == nil {
		t.Fatal("Load returned nil registry")
	}
	if len(reg.Gates) != 1 {
		t.Fatalf("expected 1 gate, got %d", len(reg.Gates))
	}
	g := reg.Gates[0]
	if g.ID != "openapi-surface" {
		t.Errorf("ID = %q; want %q", g.ID, "openapi-surface")
	}
	if g.Category != cigates.CategoryExactness {
		t.Errorf("Category = %q; want %q", g.Category, cigates.CategoryExactness)
	}
	if g.Tier != cigates.TierPrePR {
		t.Errorf("Tier = %q; want %q", g.Tier, cigates.TierPrePR)
	}
	if !g.Blocking {
		t.Error("Blocking = false; want true")
	}
	if len(g.Triggers) != 1 {
		t.Errorf("Triggers len = %d; want 1", len(g.Triggers))
	}
	if g.Local == nil {
		t.Fatal("Local is nil")
	}
	if g.Local.Command != "bash scripts/verify-openapi.sh" {
		t.Errorf("Local.Command = %q", g.Local.Command)
	}
	if g.LocalOnlyReason != "" {
		t.Errorf("LocalOnlyReason = %q; want empty", g.LocalOnlyReason)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	t.Parallel()
	_, err := cigates.Load("/nonexistent/path/ci-gates.v1.yaml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoad_DuplicateID(t *testing.T) {
	t.Parallel()
	yaml := `version: v1
gates:
  - id: dup-gate
    name: First
    category: hygiene
    tier: pre-commit
    blocking: true
    triggers: ["go/**"]
    local:
      command: "bash scripts/verify-license-header.sh"
    ci:
      workflow: test.yml
      job: "test"
    requirements: [go]
    ci_only_reason: ""
  - id: dup-gate
    name: Second
    category: hygiene
    tier: pre-commit
    blocking: true
    triggers: ["go/**"]
    local:
      command: "bash scripts/verify-license-header.sh"
    ci:
      workflow: test.yml
      job: "test"
    requirements: [go]
    ci_only_reason: ""
`
	path := writeYAML(t, yaml)
	_, err := cigates.Load(path)
	if err == nil {
		t.Fatal("expected error for duplicate id, got nil")
	}
	if !strings.Contains(err.Error(), "dup-gate") {
		t.Errorf("error %q should mention the duplicate id", err.Error())
	}
}

func TestLoad_EmptyTriggers(t *testing.T) {
	t.Parallel()
	yaml := `version: v1
gates:
  - id: no-triggers
    name: No Triggers
    category: hygiene
    tier: pre-commit
    blocking: true
    triggers: []
    local:
      command: "bash scripts/verify-license-header.sh"
    ci:
      workflow: test.yml
      job: "test"
    requirements: [go]
    ci_only_reason: ""
`
	path := writeYAML(t, yaml)
	_, err := cigates.Load(path)
	if err == nil {
		t.Fatal("expected error for empty triggers, got nil")
	}
}

func TestLoad_BadCategory(t *testing.T) {
	t.Parallel()
	yaml := `version: v1
gates:
  - id: bad-cat
    name: Bad Category
    category: notareal
    tier: pre-commit
    blocking: true
    triggers: ["go/**"]
    local:
      command: "bash scripts/verify-license-header.sh"
    ci:
      workflow: test.yml
      job: "test"
    requirements: [go]
    ci_only_reason: ""
`
	path := writeYAML(t, yaml)
	_, err := cigates.Load(path)
	if err == nil {
		t.Fatal("expected error for bad category, got nil")
	}
}

func TestLoad_BadTier(t *testing.T) {
	t.Parallel()
	yaml := `version: v1
gates:
  - id: bad-tier
    name: Bad Tier
    category: hygiene
    tier: notreal
    blocking: true
    triggers: ["go/**"]
    local:
      command: "bash scripts/verify-license-header.sh"
    ci:
      workflow: test.yml
      job: "test"
    requirements: [go]
    ci_only_reason: ""
`
	path := writeYAML(t, yaml)
	_, err := cigates.Load(path)
	if err == nil {
		t.Fatal("expected error for bad tier, got nil")
	}
}

func TestLoad_BadRequirement(t *testing.T) {
	t.Parallel()
	yaml := `version: v1
gates:
  - id: bad-req
    name: Bad Requirement
    category: hygiene
    tier: pre-commit
    blocking: true
    triggers: ["go/**"]
    local:
      command: "bash scripts/verify-license-header.sh"
    ci:
      workflow: test.yml
      job: "test"
    requirements: [notarealreq]
    ci_only_reason: ""
`
	path := writeYAML(t, yaml)
	_, err := cigates.Load(path)
	if err == nil {
		t.Fatal("expected error for bad requirement, got nil")
	}
}

func TestLoad_LocalNullWithoutReason(t *testing.T) {
	t.Parallel()
	yaml := `version: v1
gates:
  - id: ci-only-no-reason
    name: CI Only No Reason
    category: hygiene
    tier: pre-commit
    blocking: true
    triggers: ["go/**"]
    ci:
      workflow: test.yml
      job: "test"
    requirements: [go]
    ci_only_reason: ""
`
	path := writeYAML(t, yaml)
	_, err := cigates.Load(path)
	if err == nil {
		t.Fatal("expected error for local==null without ci_only_reason, got nil")
	}
}

func TestLoad_LocalNullWithReason(t *testing.T) {
	t.Parallel()
	yaml := `version: v1
gates:
  - id: ci-only-with-reason
    name: CI Only With Reason
    category: hygiene
    tier: pre-commit
    blocking: true
    triggers: ["go/**"]
    ci:
      workflow: test.yml
      job: "test"
    requirements: [go]
    ci_only_reason: "needs Postgres service"
`
	path := writeYAML(t, yaml)
	reg, err := cigates.Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if reg.Gates[0].Local != nil {
		t.Error("expected Local to be nil for CI-only gate")
	}
	if reg.Gates[0].CIOnlyReason != "needs Postgres service" {
		t.Errorf("CIOnlyReason = %q", reg.Gates[0].CIOnlyReason)
	}
}

func TestLoad_LocalOnlyReason(t *testing.T) {
	t.Parallel()
	yaml := `version: v1
gates:
  - id: local-only-proof
    name: Local Only Proof
    category: exactness
    tier: pre-pr
    blocking: true
    triggers: ["specs/local-proof.v1.yaml"]
    local:
      command: "bash scripts/verify-local-proof.sh"
    ci:
      workflow: ""
      job: ""
    requirements: [go]
    ci_only_reason: ""
    local_only_reason: "review-only local fixture until CI has an equivalent runner"
`
	path := writeYAML(t, yaml)
	reg, err := cigates.Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if got := reg.Gates[0].LocalOnlyReason; got != "review-only local fixture until CI has an equivalent runner" {
		t.Errorf("LocalOnlyReason = %q", got)
	}
}
