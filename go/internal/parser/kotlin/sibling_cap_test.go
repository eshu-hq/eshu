// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kotlin_test

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// TestKotlinPreScanIgnoresASiblingOverTheReadCap (#8986 review P2): the Kotlin
// pre-scan reads every sibling .kt in the package directory for function
// return types. A sibling over the read cap is left out of that collection, as
// the JavaScript sibling parser does, so it is not an error of the file being
// scanned: both small neighbours pre-scan normally and neither is reported as
// skipped. Not parallel: it lowers the process-wide read cap.
func TestKotlinPreScanIgnoresASiblingOverTheReadCap(t *testing.T) {
	defer shared.SetSourceReadCapForTest(64)()

	repoRoot := t.TempDir()
	a := filepath.Join(repoRoot, "pkg", "A.kt")
	b := filepath.Join(repoRoot, "pkg", "B.kt")
	c := filepath.Join(repoRoot, "pkg", "C.kt")
	writeKotlinTestFile(t, a, "package p\n\nclass A\n")
	writeKotlinTestFile(t, c, "package p\n\nclass C\n")
	writeKotlinTestFile(t, b, "package p\n\nclass B\n"+strings.Repeat("// padding line\n", 12))
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}

	for _, workers := range []int{1, 4} {
		t.Run("workers="+strconv.Itoa(workers), func(t *testing.T) {
			imports, _, skips, err := engine.PreScanRepositoryPathsSkippingOversizeStats(repoRoot, []string{a, c}, workers)
			if err != nil {
				t.Fatalf("PreScanRepositoryPathsSkippingOversizeStats() error = %v, want none", err)
			}
			if len(skips) != 0 {
				t.Fatalf("skips = %v, want none: a sibling over the cap must not blame the scanned file", skips)
			}
			parsertest.AssertPrescanContains(t, imports, "A", a)
			parsertest.AssertPrescanContains(t, imports, "C", c)
		})
	}
}
