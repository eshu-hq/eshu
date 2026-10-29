// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package apicontract

import (
	"os"
	"strings"
	"testing"
)

// TestGoModStaysStandalone is the machine-checked half of the module's
// independence contract: no require directives, so the module cannot resolve
// an eshu core version and cannot import go/internal through the build. The
// human-checked half is the import list of every non-test file (stdlib only),
// reviewed on every PR that touches this directory.
func TestGoModStaysStandalone(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "require ") || trimmed == "require" || strings.HasPrefix(trimmed, "require(") {
			t.Fatalf("go.mod must carry no require directives, found: %q", line)
		}
		if strings.Contains(line, "replace") {
			t.Fatalf("go.mod must carry no replace directives, found: %q", line)
		}
	}
	if strings.Contains(string(raw), "github.com/eshu-hq/eshu/go/internal") {
		t.Fatalf("go.mod must not reference go/internal")
	}
}
