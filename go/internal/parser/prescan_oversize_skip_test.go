// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// preScanSkipCap is the lowered read cap these tests run under.
const preScanSkipCap = 64

// preScanSkipBigBody is a valid Python source over preScanSkipCap bytes.
var preScanSkipBigBody = "def big():\n    return 1\n" + strings.Repeat("# padding line\n", 12)

// TestPreScanSkippingOversizeSkipsAFileOverTheReadCap (#8986 review P2): the
// tolerant pre-scan returns a file over the read cap as a skip and keeps
// scanning, in the sequential loop (one worker) and the concurrent loop (four).
// The strict variants still fail on it, as before. Not parallel: it lowers the
// process-wide read cap.
func TestPreScanSkippingOversizeSkipsAFileOverTheReadCap(t *testing.T) {
	defer shared.SetSourceReadCapForTest(preScanSkipCap)()

	repoRoot := t.TempDir()
	small := []string{
		filepath.Join(repoRoot, "a.py"),
		filepath.Join(repoRoot, "c.py"),
		filepath.Join(repoRoot, "d.py"),
	}
	big := filepath.Join(repoRoot, "b.py")
	for _, path := range small {
		writeTestFile(t, path, "def f_"+strings.TrimSuffix(filepath.Base(path), ".py")+"():\n    return 1\n")
	}
	writeTestFile(t, big, preScanSkipBigBody)
	paths := []string{small[0], big, small[1], small[2]}
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}

	for _, workers := range []int{1, 4} {
		t.Run("workers="+strconv.Itoa(workers), func(t *testing.T) {
			imports, stats, skips, err := engine.PreScanRepositoryPathsSkippingOversizeStats(repoRoot, paths, workers)
			if err != nil {
				t.Fatalf("PreScanRepositoryPathsSkippingOversizeStats() error = %v, want the oversize file skipped", err)
			}
			if len(skips) != 1 || skips[0].Path != big || !errors.Is(skips[0].Err, shared.ErrSourceTooLarge) {
				t.Fatalf("skips = %#v, want one skip for %s wrapping ErrSourceTooLarge", skips, big)
			}
			if len(stats) != len(small) {
				t.Fatalf("stats = %d entries, want %d (the skipped file adds none)", len(stats), len(small))
			}
			for _, name := range []string{"f_a", "f_c", "f_d"} {
				if len(imports[name]) != 1 {
					t.Errorf("imports[%q] = %v, want one path", name, imports[name])
				}
			}
			if _, ok := imports["big"]; ok {
				t.Errorf("imports names the skipped file: %v", imports["big"])
			}

			_, _, strictErr := engine.PreScanRepositoryPathsWithWorkersStats(repoRoot, paths, workers)
			if !errors.Is(strictErr, shared.ErrSourceTooLarge) {
				t.Fatalf("strict pre-scan error = %v, want it to wrap ErrSourceTooLarge", strictErr)
			}
		})
	}
}

// TestPreScanSkippingOversizeKeepsOtherErrorsFatal: only ErrSourceTooLarge is a
// per-file skip. A file that vanished still fails the scan with its error, in
// both loops, and the first error in input order wins.
func TestPreScanSkippingOversizeKeepsOtherErrorsFatal(t *testing.T) {
	repoRoot := t.TempDir()
	good := filepath.Join(repoRoot, "a.py")
	writeTestFile(t, good, "def f():\n    return 1\n")
	missing := filepath.Join(repoRoot, "gone.py")
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v", err)
	}
	for _, workers := range []int{1, 4} {
		_, _, skips, err := engine.PreScanRepositoryPathsSkippingOversizeStats(repoRoot, []string{good, missing, good}, workers)
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("workers=%d: error = %v, skips = %v, want an os.ErrNotExist failure", workers, err, skips)
		}
	}
}
