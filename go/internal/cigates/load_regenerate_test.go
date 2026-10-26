// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"os"
	"path/filepath"
	"regexp"
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

// TestValidate_RegenerateGlobCommandScriptMissing is the glob half of
// TestValidate_RegenerateCommandScriptMissing: the scripts/ existence check
// runs before the glob/literal split, so a typo'd generator behind a glob
// key fails here rather than at the next rebase conflict.
func TestValidate_RegenerateGlobCommandScriptMissing(t *testing.T) {
	t.Parallel()
	root := buildHermeticRepo(
		t,
		[]string{"scripts/generate-thing-doc.sh"},
		[]string{"static-contract-gates.yml"},
	)
	writeTracked(t, root, "docs/public/reference/pages/a.md")
	yaml := strings.Replace(regenerateGateYAML,
		`      "docs/public/reference/thing.md": "bash scripts/generate-thing-doc.sh"`,
		`      "docs/public/reference/pages/*.md": "bash scripts/generate-missing-doc.sh"`, 1)
	path := writeYAML(t, yaml)
	reg, err := cigates.Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	errs := reg.Validate(root)
	if got := errorNaming(errs, "gen-doc", "generate-missing-doc.sh"); got == "" {
		t.Fatalf("Validate() errors = %v, want one naming the gate and the missing regenerate script behind a glob key", errs)
	}
}

// TestValidate_RegenerateCommandQuoteOrBackslashFails pins the raw-text
// parity rule: the helper parses fragment text while Load sees the decoded
// value, so a regenerate command carrying a double quote or backslash would
// run one thing locally and validate another.
func TestValidate_RegenerateCommandQuoteOrBackslashFails(t *testing.T) {
	t.Parallel()
	for _, command := range []string{
		`bash scripts/generate-thing-doc.sh --title "thing"`,
		`bash scripts/generate-thing-doc.sh --out docs\\thing.md`,
	} {
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
		reg.Gates[0].Regenerate["docs/public/reference/thing.md"] = command

		errs := reg.Validate(root)
		if got := errorNaming(errs, "gen-doc", "misread"); got == "" {
			t.Fatalf("Validate() errors = %v, want a misread refusal for command %q", errs, command)
		}
	}
}

// TestRegenerateEntriesMatchHelperShape scans the committed fragments'
// raw text and asserts every regenerate: block holds only lines the
// helper's awk parser accepts (`      "path": "command"` with no quotes
// or backslashes). Load sees decoded values, so only a raw-text scan can
// catch a shape the decoder accepts but the helper misreads — a
// single-quoted entry, a flow-form map, or a backslash escape. At least
// one entry must exist, so a wholesale shape drift cannot pass vacuously.
func TestRegenerateEntriesMatchHelperShape(t *testing.T) {
	t.Parallel()
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	registryPath := filepath.Join(repoRoot, "specs", "ci-gates.v1.yaml")
	raw, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	var fragments []string
	inList := false
	for _, line := range strings.Split(string(raw), "\n") {
		switch {
		case line == "gate_fragments:":
			inList = true
		case inList && strings.HasPrefix(line, "  - "):
			fragments = append(fragments, strings.TrimPrefix(line, "  - "))
		case inList && line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "#"):
			inList = false
		}
	}
	if len(fragments) == 0 {
		t.Fatal("no gate fragments listed in the committed registry")
	}
	entryLine := regexp.MustCompile(`^      "[^"]+": "[^"]*"$`)
	found := 0
	for _, ref := range fragments {
		body, err := os.ReadFile(filepath.Join(repoRoot, "specs", ref))
		if err != nil {
			t.Fatalf("read fragment %s: %v", ref, err)
		}
		inRegen := false
		for i, line := range strings.Split(string(body), "\n") {
			switch {
			case line == "    regenerate:":
				inRegen = true
			case strings.HasPrefix(line, "    regenerate: ") || strings.HasPrefix(line, "    regenerate:\t"):
				t.Errorf("fragment %s line %d: flow-form regenerate value the helper cannot parse: %q", ref, i+1, line)
				inRegen = false
			case inRegen && (line == "" || strings.HasPrefix(line, "      #")):
				// Blank lines and comments do not close the block,
				// mirroring the helper's awk state machine.
			case inRegen && strings.HasPrefix(line, "      "):
				if !entryLine.MatchString(line) || strings.Contains(line, "\\") {
					t.Errorf("fragment %s line %d: not a helper-shaped entry: %q", ref, i+1, line)
				} else {
					found++
				}
			case inRegen:
				// Any dedent (next gate, next key, section comment)
				// closes the block.
				inRegen = false
			}
		}
	}
	if found == 0 {
		t.Error("no helper-shaped regenerate entries in the committed registry; the scan is vacuous")
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
