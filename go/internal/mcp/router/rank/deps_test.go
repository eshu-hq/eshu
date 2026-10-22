// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank

import (
	"os/exec"
	"strings"
	"testing"
)

// TestRankDoesNotImportMCPRoot guards the import direction. The mcp root
// injects the ranker, so importing the root back would be a cycle. -test also
// covers _test.go imports.
func TestRankDoesNotImportMCPRoot(t *testing.T) {
	t.Parallel()
	out, err := exec.Command("go", "list", "-test", "-deps", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps: %v\n%s", err, out)
	}
	for _, dep := range strings.Fields(string(out)) {
		if dep == "github.com/eshu-hq/eshu/go/internal/mcp" {
			t.Fatalf("rank depends on the mcp root package")
		}
	}
}
