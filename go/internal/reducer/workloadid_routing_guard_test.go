// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestWorkloadIDsRouteThroughConstructors is the committed guard behind the
// "compiler enumerates construction sites" claim (#6580 P2). The opaque
// workloadid types stop direct conversions, but a new
// fmt.Sprintf("workload:...") or "workload:" + name site in this package
// would still compile and silently reintroduce hand-built identifiers, and
// the rows-track test passes byte-identical reintroductions by design. So
// this test scans the package's own non-test sources for the three hand-built
// shapes and fails on any.
//
// Deliberately narrow patterns, not a broad "workload:" match:
//   - `Sprintf("workload:%s",` is the exact pre-refactor workload construction. The
//     `workload:%s->%s` partition keys in dependency_domain.go do not
//     contain this substring and stay green.
//   - `Sprintf("workload-instance:` is the exact pre-refactor instance
//     construction; the workload pattern above cannot match it because
//     `workload-` follows `Sprintf("`, not `workload:` (#6580 P2 — the
//     guard first shipped without this shape and would have missed two
//     of the four removed lines).
//   - `"workload:" +` is concatenation construction. Zero hits today.
//
// Test files are out of scope: fixtures legitimately quote identifier
// literals as expected values, and test code does not ship.
func TestWorkloadIDsRouteThroughConstructors(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller cannot locate this test file")
	}
	dir := filepath.Dir(thisFile)

	forbidden := []string{
		`Sprintf("workload:%s",`,
		`Sprintf("workload-instance:`,
		`"workload:" +`,
	}

	var violations []string
	walkErr := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("reading %s: %v", path, readErr)
		}
		for _, line := range strings.Split(string(content), "\n") {
			for _, pattern := range forbidden {
				if strings.Contains(line, pattern) {
					violations = append(violations, path+": "+strings.TrimSpace(line))
				}
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walking %s: %v", dir, walkErr)
	}
	for _, violation := range violations {
		t.Errorf("hand-built workload identifier construction (route through workloadid instead): %s", violation)
	}
}
