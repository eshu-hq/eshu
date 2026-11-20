// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestGenerateLeafFiltersCommand regenerates both shared leaf filters from
// fixture specs and proves the written files match the generator contract:
// the code filter negates the patterns, the lockstep filter lists the
// manifest verbatim.
func TestGenerateLeafFiltersCommand(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("specs/ci-docs-set-patterns.txt", "# comment\n\ndocs/**\n**/*.md\n")
	write("specs/ci-docs-lockstep-paths.txt", "AGENTS.md\n")
	bin := buildBinary(t)
	cmd := exec.Command(bin, "generate-leaf-filters", "--repo-root", root)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate-leaf-filters failed: %v\n%s", err, out)
	}
	code, err := os.ReadFile(filepath.Join(root, ".github", "leaf-code-filter.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"code:\n", "  - '**'\n", "  - '!docs/**'\n", "  - '!*.md'\n", "  - '!**/*.md'\n"} {
		if !strings.Contains(string(code), want) {
			t.Errorf("generated code filter lacks %q:\n%s", want, code)
		}
	}
	lockstep, err := os.ReadFile(filepath.Join(root, ".github", "leaf-lockstep-filter.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(lockstep), "lockstep:\n  - 'AGENTS.md'\n") {
		t.Errorf("generated lockstep filter lacks the manifest entry:\n%s", lockstep)
	}
}

// TestGenerateLeafFiltersCommandFailsClosed proves a specs edit that empties
// the pattern set fails the command instead of writing an unnegated filter.
func TestGenerateLeafFiltersCommandFailsClosed(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "specs", "ci-docs-set-patterns.txt"), []byte("# only a comment\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "specs", "ci-docs-lockstep-paths.txt"), []byte("AGENTS.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := buildBinary(t)
	cmd := exec.Command(bin, "generate-leaf-filters", "--repo-root", root)
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("generate-leaf-filters succeeded on an empty pattern set, output:\n%s", out)
	}
}
