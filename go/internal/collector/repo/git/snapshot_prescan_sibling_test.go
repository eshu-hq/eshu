// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// TestSnapshotDoesNotBlameKotlinNeighboursForAnOversizeSibling (#8986 review
// P2): the Kotlin pre-scan reads every sibling .kt in the package directory. An
// oversize sibling (B.kt, over the cap at discovery) is left out of that read,
// so the small neighbours A.kt and C.kt are neither listed as oversize, nor
// dropped from the parse, nor tombstoned on a delta, nor marked degraded, and a
// second sync converges to the same result. Not parallel: it lowers the
// process-wide read cap.
func TestSnapshotDoesNotBlameKotlinNeighboursForAnOversizeSibling(t *testing.T) {
	defer shared.SetSourceReadCapForTest(oversizeTestCap)()

	bigBody := "package p\n\nclass B\n" + strings.Repeat("// padding line\n", 12)
	for _, workers := range []int{1, 4} {
		for _, delta := range []bool{false, true} {
			name := "workers=" + strconv.Itoa(workers) + "/full"
			if delta {
				name = "workers=" + strconv.Itoa(workers) + "/delta"
			}
			t.Run(name, func(t *testing.T) {
				repoRoot := t.TempDir()
				writeCollectorTestFile(t, filepath.Join(repoRoot, "pkg", "A.kt"), "package p\n\nclass A\n")
				writeCollectorTestFile(t, filepath.Join(repoRoot, "pkg", "C.kt"), "package p\n\nclass C\n")
				writeCollectorTestFile(t, filepath.Join(repoRoot, "pkg", "B.kt"), bigBody)
				repository := SelectedRepository{RepoPath: repoRoot}
				if delta {
					repository.Delta = true
					repository.FileTargets = []string{filepath.Join(repoRoot, "pkg", "A.kt"), filepath.Join(repoRoot, "pkg", "B.kt")}
				}
				snapshotter := NativeRepositorySnapshotter{Engine: defaultCollectorTestEngine(t), ParseWorkers: workers}
				for sync := 1; sync <= 2; sync++ {
					ctx, degradation := withSnapshotDegradation(context.Background())
					got, err := snapshotter.SnapshotRepository(ctx, repository)
					if err != nil {
						t.Fatalf("sync %d: SnapshotRepository() error = %v", sync, err)
					}
					want := SkippedOversizeFile{RelativePath: "pkg/B.kt", SizeBytes: int64(len(bigBody))}
					if len(got.OversizeSkippedFiles) != 1 || got.OversizeSkippedFiles[0] != want {
						t.Fatalf("sync %d: OversizeSkippedFiles = %#v, want only [%#v]", sync, got.OversizeSkippedFiles, want)
					}
					parsed := map[string]bool{}
					for _, fileData := range got.FileData {
						parsed[filepath.Base(model.PayloadPath(fileData, "path"))] = true
					}
					if !parsed["A.kt"] || parsed["B.kt"] || (!delta && !parsed["C.kt"]) {
						t.Fatalf("sync %d: parsed files = %v, want A.kt (and C.kt in full mode) parsed and B.kt skipped", sync, parsed)
					}
					if len(got.DeletedRelativePaths) != map[bool]int{false: 0, true: 1}[delta] {
						t.Fatalf("sync %d: DeletedRelativePaths = %v, want only the oversize delta target B.kt", sync, got.DeletedRelativePaths)
					}
					if reason, degraded := degradation.degraded(); degraded {
						t.Fatalf("sync %d: snapshot degraded (%q), want a clean snapshot", sync, reason)
					}
				}
			})
		}
	}
}

// TestSnapshotSkipsAGoFileThatGrewPastTheCapBeforePreScan (#8986 review P2):
// the Go package pre-scan reads every Go file after the import pre-scan, so a
// grown file the import pre-scan skipped must be left out of it too, or the
// snapshot still fails with the read-cap error. The skip is also counted in the
// discovery advisory under content:oversize. Not parallel: it lowers the
// process-wide read cap and swaps gitCommitSHAFn.
func TestSnapshotSkipsAGoFileThatGrewPastTheCapBeforePreScan(t *testing.T) {
	defer shared.SetSourceReadCapForTest(oversizeTestCap)()

	grownBody := "package main\n\nfunc grow() {}\n" + strings.Repeat("// padding line\n", 12)
	for _, workers := range []int{1, 4} {
		t.Run("workers="+strconv.Itoa(workers), func(t *testing.T) {
			repoRoot := t.TempDir()
			for _, name := range []string{"a.go", "b.go", "grow.go"} {
				writeCollectorTestFile(t, filepath.Join(repoRoot, name), "package main\n\nfunc "+strings.TrimSuffix(name, ".go")+"() {}\n")
			}
			defer growBetweenDiscoveryAndPreScan(t, filepath.Join(repoRoot, "grow.go"), grownBody)()
			snapshotter := NativeRepositorySnapshotter{Engine: defaultCollectorTestEngine(t), ParseWorkers: workers}
			got, err := snapshotter.SnapshotRepository(context.Background(), SelectedRepository{RepoPath: repoRoot})
			if err != nil {
				t.Fatalf("SnapshotRepository() error = %v, want the grown Go file skipped without failing the snapshot", err)
			}
			want := SkippedOversizeFile{RelativePath: "grow.go", SizeBytes: int64(len(grownBody))}
			if len(got.OversizeSkippedFiles) != 1 || got.OversizeSkippedFiles[0] != want {
				t.Fatalf("OversizeSkippedFiles = %#v, want [%#v]", got.OversizeSkippedFiles, want)
			}
			if got.DiscoveryAdvisory == nil || got.DiscoveryAdvisory.SkipBreakdown.FilesByContent[oversizeSkipReason] != 1 {
				t.Fatalf("DiscoveryAdvisory skip breakdown = %+v, want content:oversize=1 for the grown file", got.DiscoveryAdvisory)
			}
		})
	}
}
