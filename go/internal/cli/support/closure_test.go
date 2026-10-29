// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package support

import (
	"os/exec"
	"strings"
	"testing"
)

// apiBinaryClosure lists the production dependency closure of the api
// binary, one "<importpath> <direct imports...>" row per package. The
// golden-corpus filter exclusions for the S4 kernels rest on the claim that
// the support leaf is the only cli/* package the api binary links and that
// no gate-asserted route can reach the answerquality/reportbundle kernels
// except through it; this test re-checks that claim on every run so a new
// importer fails here instead of silently widening gate-blind code (the
// incident class behind #5877).
func apiBinaryClosure(t *testing.T) []string {
	t.Helper()
	out, err := exec.Command("go", "list", "-deps",
		"-f", "{{.ImportPath}} {{join .Imports \" \"}}",
		"github.com/eshu-hq/eshu/go/cmd/api").CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps cmd/api: %v\n%s", err, out)
	}
	return strings.Split(string(out), "\n")
}

// TestAPIServerLinksOnlyCLISupportLeaf pins the corpus-filter exclusion for
// go/internal/cli: within the api binary, the support leaf is the only
// cli/* package present.
func TestAPIServerLinksOnlyCLISupportLeaf(t *testing.T) {
	t.Parallel()
	for _, row := range apiBinaryClosure(t) {
		path := strings.SplitN(row, " ", 2)[0]
		if !strings.HasPrefix(path, "github.com/eshu-hq/eshu/go/internal/cli/") {
			continue
		}
		if path != "github.com/eshu-hq/eshu/go/internal/cli/support" {
			t.Fatalf("api binary links cli package %s, want only cli/support", path)
		}
	}
}

// TestAPIServerKernelsReachableOnlyThroughSupport pins the corpus-filter
// exclusions for the answerquality and reportbundle kernels: no package in
// the api binary outside the support leaf may import them directly, so a
// gate-asserted route can never reach kernel code the gate does not assert.
func TestAPIServerKernelsReachableOnlyThroughSupport(t *testing.T) {
	t.Parallel()
	kernels := map[string]bool{
		"github.com/eshu-hq/eshu/go/internal/answerquality": true,
		"github.com/eshu-hq/eshu/go/internal/reportbundle":  true,
	}
	for _, row := range apiBinaryClosure(t) {
		fields := strings.Fields(row)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "github.com/eshu-hq/eshu/go/internal/cli/support" {
			continue
		}
		for _, imp := range fields[1:] {
			if kernels[imp] {
				t.Fatalf("api package %s imports kernel %s outside cli/support", fields[0], imp)
			}
		}
	}
}
