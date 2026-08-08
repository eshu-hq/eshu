// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"path/filepath"
	"testing"
)

// TestGoPackageGateCount pins goPackageGateCount to the committed registry,
// derived through extractGoPackageDirs — the production extractor this check
// uses — so the constant and the check can never disagree about what counts as
// a Go-package gate.
//
// This is the guard the first version of gopkgtrigger.go's doc comment lacked.
// It said 19, a number taken from a different measurement (gates with no
// scripts/ token, which includes the npm gates) and never re-derived. A
// reviewer caught it. The same class had already cost this package once, in
// checkVerifyScriptWorkflowMatch's "29 gates".
func TestGoPackageGateCount(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	reg, err := Load(filepath.Join(repoRoot, "specs", "ci-gates.v1.yaml"))
	if err != nil {
		t.Fatalf("Load(specs/ci-gates.v1.yaml): %v", err)
	}

	got := 0
	for _, gate := range reg.Gates {
		if gate.Local == nil {
			continue
		}
		if len(extractGoPackageDirs(gate.Local.Command+" "+gate.Local.TestCommand)) > 0 {
			got++
		}
	}

	if got != goPackageGateCount {
		t.Errorf("goPackageGateCount is %d, but the committed registry currently derives %d "+
			"local gates running a Go package; update the constant in gopkgtrigger.go to match",
			goPackageGateCount, got)
	}
}
