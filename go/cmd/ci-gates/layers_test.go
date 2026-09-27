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

// TestValidateSubcommand_RequiresPurpose proves `validate` wires in
// DescriptionCheck: the real registry with one purpose line removed must fail.
func TestValidateSubcommand_RequiresPurpose(t *testing.T) {
	t.Parallel()
	bin := buildBinary(t)
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "specs", "ci-gates.v1.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(raw), "\n")
	removed := ""
	for i, line := range lines {
		if strings.HasPrefix(line, "    purpose: ") {
			removed = line
			lines = append(lines[:i], lines[i+1:]...)
			break
		}
	}
	if removed == "" {
		t.Fatal("real registry has no purpose line to remove")
	}
	regPath := writeRegistry(t, t.TempDir(), strings.Join(lines, "\n"))

	cmd := exec.Command(bin, "validate", "--registry", regPath, "--repo-root", root)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("validate passed with a purpose removed (%s); want a failure\n%s", removed, out)
	}
	if !strings.Contains(string(out), "has no purpose") {
		t.Fatalf("validate output should name the missing purpose:\n%s", out)
	}
}

// TestLayersSubcommand_RendersRealRegistry proves the `layers` subcommand
// prints the by-layer section and that every real gate carries a layer.
func TestLayersSubcommand_RendersRealRegistry(t *testing.T) {
	t.Parallel()
	bin := buildBinary(t)
	regPath := filepath.Join(repoRoot(t), "specs", "ci-gates.v1.yaml")

	out, err := exec.Command(bin, "layers", "--registry", regPath).CombinedOutput()
	if err != nil {
		t.Fatalf("layers failed: %v\n%s", err, out)
	}
	for _, want := range []string{"## Gates by layer", "### Hygiene: ", "### Truth: ", "### Performance: "} {
		if !strings.Contains(string(out), want) {
			t.Errorf("layers output missing %q", want)
		}
	}
	if strings.Contains(string(out), "### Unlabeled") {
		t.Errorf("layers output has an Unlabeled section; every real gate must carry a layer:\n%s", out)
	}
}
