// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

const regenerateGateYAML = `version: v1
gates:
  - id: gen-doc
    name: Generated doc
    category: hygiene
    tier: pre-pr
    blocking: true
    triggers:
      - "docs/public/reference/thing.md"
    local:
      command: "bash scripts/generate-thing-doc.sh"
    ci:
      workflow: static-contract-gates.yml
      job: "Verify thing gate"
    requirements: []
    ci_only_reason: ""
    local_only_reason: ""
    regenerate:
      "docs/public/reference/thing.md": "bash scripts/generate-thing-doc.sh"
`

// TestLoad_RegenerateFieldMapped pins that a gate's regenerate: map survives
// Load onto the public Gate: scripts/dev/regen-conflicts.sh reads the same
// field from the YAML, so a Load that dropped it would leave the Go view and
// the helper's view of the registry silently disagreeing.
func TestLoad_RegenerateFieldMapped(t *testing.T) {
	t.Parallel()
	path := writeYAML(t, regenerateGateYAML)
	reg, err := cigates.Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(reg.Gates) != 1 {
		t.Fatalf("expected 1 gate, got %d", len(reg.Gates))
	}
	got := reg.Gates[0].Regenerate
	if len(got) != 1 {
		t.Fatalf("Regenerate = %v; want one path-to-command entry", got)
	}
	if got["docs/public/reference/thing.md"] != "bash scripts/generate-thing-doc.sh" {
		t.Errorf("Regenerate entry = %q; want the generator command", got["docs/public/reference/thing.md"])
	}
}

// TestValidate_RegenerateCommandScriptMissing pins the fail-closed side: a
// regenerate command naming a scripts/ file that is not on disk must fail
// Validate, or a typo'd generator command reads as wired until a rebase
// conflict tries to run it.
func TestValidate_RegenerateCommandScriptMissing(t *testing.T) {
	t.Parallel()
	root := buildHermeticRepo(
		t,
		[]string{"scripts/generate-thing-doc.sh"},
		[]string{"static-contract-gates.yml"},
	)
	writeTracked(t, root, "docs/public/reference/thing.md")
	path := writeYAML(t, regenerateGateYAML)
	reg, err := cigates.Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	// Point the regenerate command at a script that does not exist.
	reg.Gates[0].Regenerate["docs/public/reference/thing.md"] = "bash scripts/generate-thing-doc-missing.sh"

	errs := reg.Validate(root)
	if got := errorNaming(errs, "gen-doc", "generate-thing-doc-missing.sh"); got == "" {
		t.Fatalf("Validate() errors = %v, want one naming the gate and the missing regenerate script", errs)
	}
}

// TestValidate_RegenerateDuplicatePathAcrossGates pins single ownership: two
// gates claiming the same generated path would let two regen commands fight
// over one conflict, so Validate must refuse the registry.
func TestValidate_RegenerateDuplicatePathAcrossGates(t *testing.T) {
	t.Parallel()
	root := buildHermeticRepo(
		t,
		[]string{"scripts/generate-thing-doc.sh", "scripts/generate-other-doc.sh"},
		[]string{"static-contract-gates.yml"},
	)
	writeTracked(t, root, "docs/public/reference/thing.md")
	yaml := regenerateGateYAML + `  - id: other-doc
    name: Other doc
    category: hygiene
    tier: pre-pr
    blocking: true
    triggers:
      - "docs/public/reference/thing.md"
    local:
      command: "bash scripts/generate-other-doc.sh"
    ci:
      workflow: static-contract-gates.yml
      job: "Verify other gate"
    requirements: []
    ci_only_reason: ""
    local_only_reason: ""
    regenerate:
      "docs/public/reference/thing.md": "bash scripts/generate-other-doc.sh"
`
	path := writeYAML(t, yaml)
	reg, err := cigates.Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	errs := reg.Validate(root)
	found := false
	for _, err := range errs {
		msg := err.Error()
		if strings.Contains(msg, "docs/public/reference/thing.md") &&
			strings.Contains(msg, "gen-doc") && strings.Contains(msg, "other-doc") {
			found = true
		}
	}
	if !found {
		t.Fatalf("Validate() errors = %v, want one naming the doubly-claimed path and both gates", errs)
	}
}

// TestValidate_RegenerateLiteralPathMissing pins that a literal regenerate
// path must exist: a key for a generated file the tree no longer has is a
// stale claim, not coverage. The trigger points at a real file so the only
// missing-path error can come from the regenerate half, not the trigger half.
func TestValidate_RegenerateLiteralPathMissing(t *testing.T) {
	t.Parallel()
	root := buildHermeticRepo(
		t,
		[]string{"scripts/generate-thing-doc.sh"},
		[]string{"static-contract-gates.yml"},
	)
	writeTracked(t, root, "docs/public/reference/trigger.md")
	yaml := strings.Replace(regenerateGateYAML,
		`      - "docs/public/reference/thing.md"`,
		`      - "docs/public/reference/trigger.md"`, 1)
	// NOTE: docs/public/reference/thing.md deliberately not written.
	path := writeYAML(t, yaml)
	reg, err := cigates.Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	errs := reg.Validate(root)
	if got := errorNaming(errs, "gen-doc", "regenerate", "docs/public/reference/thing.md"); got == "" {
		t.Fatalf("Validate() errors = %v, want one naming the gate and the missing generated path", errs)
	}
}

// TestValidate_RegenerateEmptyCommandFails pins that a regenerate entry with
// an empty command is rejected: an empty string would read as "no regen
// needed" while the path stays conflicted.
func TestValidate_RegenerateEmptyCommandFails(t *testing.T) {
	t.Parallel()
	root := buildHermeticRepo(
		t,
		[]string{"scripts/generate-thing-doc.sh"},
		[]string{"static-contract-gates.yml"},
	)
	writeTracked(t, root, "docs/public/reference/thing.md")
	path := writeYAML(t, regenerateGateYAML)
	reg, err := cigates.Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	reg.Gates[0].Regenerate["docs/public/reference/thing.md"] = "   "

	errs := reg.Validate(root)
	if got := errorNaming(errs, "gen-doc", "docs/public/reference/thing.md"); got == "" {
		t.Fatalf("Validate() errors = %v, want one naming the gate and the empty-command path", errs)
	}
}
