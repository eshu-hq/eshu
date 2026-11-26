// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// A Python test mirror reaches its case modules by import, not by shell
// `source`. Before #9479 the trigger walk followed only `source` edges, so a
// PR that edited only an imported module (the readiness row budget in
// scripts/lib/live_postgres_readiness_row_split.py) selected no gate and its
// first run was in CI. The walk must follow imports transitively.
func TestDriftCheck_PythonImportedModuleNotCoveredByOwnTriggers(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), []string{"verify.yml"})
	writeScript(t, root, "scripts/test-thing.py",
		"import sys\n"+
			"sys.path.insert(0, 'scripts/lib')\n"+
			"import thing_level1 as level1  # noqa: E402\n"+
			"def helper():\n"+
			"    from thing_inline import value\n"+
			"    return value\n")
	writeScript(t, root, "scripts/lib/thing_level1.py", "import json, thing_level2\n")
	writeScript(t, root, "scripts/lib/thing_level2.py", "VALUE = 1\n")
	writeScript(t, root, "scripts/lib/thing_inline.py", "value = 2\n")

	g := gateWith("my-gate", "my-gate", "verify.yml")
	g.Triggers = []string{
		"go/**",
		"scripts/test-thing.py",
		"scripts/lib/thing_level1.py",
		// thing_level2.py (imported by level 1) and thing_inline.py (an
		// indented from-import) are deliberately absent.
	}
	g.Local = &cigates.Local{Command: "python3 scripts/test-thing.py"}

	errs := scriptTriggerErrs(t, root, minimalReg([]cigates.Gate{g}, nil, nil))
	if len(errs) != 2 {
		t.Fatalf("expected 2 imported-module errors, got %d: %v", len(errs), errs)
	}
	joined := strings.Join(errs, "\n")
	for _, want := range []string{
		`imports "scripts/lib/thing_inline.py"`,
		`imports "scripts/lib/thing_level2.py"`,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("errors should contain %s, got:\n%s", want, joined)
		}
	}
}

// Standard-library, dotted, relative, and unresolvable imports name no
// scripts/ file, so they add no edge and no finding.
func TestDriftCheck_PythonImportsWithoutAScriptsModuleIgnored(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("my-gate"), []string{"verify.yml"})
	writeScript(t, root, "scripts/test-thing.py",
		"import json\n"+
			"import os.path\n"+
			"from unittest import mock\n"+
			"from . import sibling\n"+
			"import not_on_disk as gone\n"+
			"text = '''\nnot code\n'''\n")

	g := gateWith("my-gate", "my-gate", "verify.yml")
	g.Triggers = []string{"go/**", "scripts/test-thing.py"}
	g.Local = &cigates.Local{Command: "python3 scripts/test-thing.py"}

	if errs := scriptTriggerErrs(t, root, minimalReg([]cigates.Gate{g}, nil, nil)); len(errs) != 0 {
		t.Fatalf("expected no findings, got %d: %v", len(errs), errs)
	}
	// All checks above are self-contained unit tests for Python import traversal.
}
