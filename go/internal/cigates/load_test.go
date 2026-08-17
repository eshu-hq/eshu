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

// A repeated path inside one gate's own triggers: block was invisible before
// this check: Load only ever validated that the id set had no duplicates
// (TestLoad_DuplicateID above), never that a trigger LIST was actually a SET.
// A copy-paste of a trigger line -- or a near-miss merge of two sibling
// families' trigger blocks -- silently doubled an entry with no error and no
// functional effect on matching (MatchGlob against a duplicated glob behaves
// identically to matching it once), so it could go unnoticed indefinitely.
func TestLoad_DuplicateTrigger(t *testing.T) {
	t.Parallel()
	yaml := `version: v1
gates:
  - id: dup-trigger-gate
    name: Duplicate Trigger
    category: hygiene
    tier: pre-commit
    blocking: true
    triggers:
      - "go/**"
      - "scripts/verify-thing.sh"
      - "go/**"
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
		t.Fatal("expected error for duplicate trigger entry, got nil")
	}
	if !strings.Contains(err.Error(), "dup-trigger-gate") || !strings.Contains(err.Error(), "go/**") {
		t.Errorf("error %q should name the gate and the duplicated trigger", err.Error())
	}
}

// A blocking:false gate with no ci.workflow/ci.job has no CI backstop at
// all: for every other gate a local skip is harmless because CI backstops
// it, but a gate that runs ONLY through a developer's local `make pre-pr`
// and is never wired into CI can go unexercised indefinitely if nobody
// happens to run it by hand -- root-cause-evidence, exactly this shape,
// had never run against any evidence doc until it was run by hand (#6149
// follow-up item 5). local_only_reason already exists and three of the four
// real gates in this shape use it to declare the gap as a deliberate,
// temporary staging decision rather than an accident -- but nothing
// enforced that every gate in this shape declare one. This is the
// enforcement: a blocking:false gate whose ci.workflow AND ci.job are both
// empty must carry a non-empty local_only_reason, mirroring the existing
// ci_only_reason-required-when-local-is-nil rule exactly.
func TestLoad_NoCIBackstopWithoutLocalOnlyReason(t *testing.T) {
	t.Parallel()
	yaml := `version: v1
gates:
  - id: undeclared-local-only
    name: Undeclared Local Only
    category: hygiene
    tier: pre-pr
    blocking: false
    triggers: ["go/**"]
    local:
      command: "bash scripts/verify-license-header.sh"
    ci:
      workflow: ""
      job: ""
    requirements: [go]
    ci_only_reason: ""
`
	path := writeYAML(t, yaml)
	_, err := cigates.Load(path)
	if err == nil {
		t.Fatal("expected error for a blocking:false gate with no CI backstop and no local_only_reason, got nil")
	}
	if !strings.Contains(err.Error(), "undeclared-local-only") || !strings.Contains(err.Error(), "local_only_reason") {
		t.Errorf("error %q should name the gate and mention local_only_reason", err.Error())
	}
}

// The same shape with a non-empty local_only_reason is accepted: the whole
// point is to require the DECLARATION, not to forbid the gap outright (three
// real gates rely on exactly this staged shape today).
func TestLoad_NoCIBackstopWithLocalOnlyReasonAccepted(t *testing.T) {
	t.Parallel()
	yaml := `version: v1
gates:
  - id: declared-local-only
    name: Declared Local Only
    category: hygiene
    tier: pre-pr
    blocking: false
    triggers: ["go/**"]
    local:
      command: "bash scripts/verify-license-header.sh"
    ci:
      workflow: ""
      job: ""
    requirements: [go]
    ci_only_reason: ""
    local_only_reason: "advisory baseline, CI wiring tracked separately"
`
	path := writeYAML(t, yaml)
	reg, err := cigates.Load(path)
	if err != nil {
		t.Fatalf("Load returned error for a declared local-only gate: %v", err)
	}
	if got := reg.Gates[0].LocalOnlyReason; got != "advisory baseline, CI wiring tracked separately" {
		t.Errorf("LocalOnlyReason = %q", got)
	}
}

// A blocking gate with a real CI backstop needs no local_only_reason at all
// -- this rule must not regress the common case.
func TestLoad_BlockingGateWithCIBackstopNeedsNoLocalOnlyReason(t *testing.T) {
	t.Parallel()
	yaml := `version: v1
gates:
  - id: blocking-with-ci
    name: Blocking With CI
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
	if _, err := cigates.Load(path); err != nil {
		t.Fatalf("Load returned error for a blocking gate with a real CI backstop: %v", err)
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
