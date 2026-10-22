// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import (
	"os/exec"
	"strings"
	"testing"
)

// TestRouterDoesNotImportMCPRoot guards the import direction: the mcp root
// injects a Dispatcher into this package, so importing the root back would be
// a cycle.
func TestRouterDoesNotImportMCPRoot(t *testing.T) {
	t.Parallel()
	out, err := exec.Command("go", "list", "-deps", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps: %v\n%s", err, out)
	}
	for _, dep := range strings.Fields(string(out)) {
		if dep == "github.com/eshu-hq/eshu/go/internal/mcp" {
			t.Fatalf("router depends on the mcp root package")
		}
	}
}
