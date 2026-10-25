// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// growBetweenDiscoveryAndPreScan swaps the gitCommitSHAFn seam, which the
// snapshot calls after discovery sized the files and before the import
// pre-scan reads them, so the named file grows past the read cap in exactly
// that window. It returns the restore function.
func growBetweenDiscoveryAndPreScan(t *testing.T, path, body string) func() {
	t.Helper()
	original := gitCommitSHAFn
	gitCommitSHAFn = func(context.Context, string) string {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Errorf("grow %s: %v", path, err)
		}
		return "c"
	}
	return func() { gitCommitSHAFn = original }
}

// writeGrowthRepo lays out three small Python files, all under the read cap
// when discovery sizes them. Three files keep the worker-aware pre-scan on its
// concurrent path.
func writeGrowthRepo(t *testing.T) string {
	t.Helper()
	repoRoot := t.TempDir()
	for _, name := range []string{"app.py", "other.py", "grow.py"} {
		writeCollectorTestFile(t, filepath.Join(repoRoot, name), "def f():\n    return 1\n")
	}
	return repoRoot
}

// TestSnapshotSkipsAFileThatGrewPastTheCapBeforePreScan (#8986 review P2): a
// parser file under the cap at discovery that grows past it before the import
// pre-scan reads it is skipped like an oversize file, not a failure of the
// whole snapshot. It is absent from the snapshot, listed in
// OversizeSkippedFiles, and a delta target is retracted. The sequential and the
// worker-aware pre-scan loops both apply. Not parallel: it lowers the
// process-wide read cap and swaps gitCommitSHAFn.
func TestSnapshotSkipsAFileThatGrewPastTheCapBeforePreScan(t *testing.T) {
	defer shared.SetSourceReadCapForTest(oversizeTestCap)()

	for _, workers := range []int{1, 4} {
		for _, delta := range []bool{false, true} {
			name := "workers=" + strconv.Itoa(workers) + "/full"
			if delta {
				name = "workers=" + strconv.Itoa(workers) + "/delta"
			}
			t.Run(name, func(t *testing.T) {
				repoRoot := writeGrowthRepo(t)
				defer growBetweenDiscoveryAndPreScan(t, filepath.Join(repoRoot, "grow.py"), oversizeTestBody)()
				repository := SelectedRepository{RepoPath: repoRoot}
				if delta {
					repository.Delta = true
					repository.FileTargets = []string{filepath.Join(repoRoot, "app.py"), filepath.Join(repoRoot, "grow.py")}
				}
				snapshotter := NativeRepositorySnapshotter{Engine: defaultCollectorTestEngine(t), ParseWorkers: workers}
				got, err := snapshotter.SnapshotRepository(context.Background(), repository)
				if err != nil {
					t.Fatalf("SnapshotRepository() error = %v, want the grown file skipped without failing the snapshot", err)
				}

				want := SkippedOversizeFile{RelativePath: "grow.py", SizeBytes: int64(len(oversizeTestBody))}
				if len(got.OversizeSkippedFiles) != 1 || got.OversizeSkippedFiles[0] != want {
					t.Fatalf("OversizeSkippedFiles = %#v, want [%#v]", got.OversizeSkippedFiles, want)
				}
				parsed := map[string]bool{}
				for _, fileData := range got.FileData {
					parsed[filepath.Base(model.PayloadPath(fileData, "path"))] = true
				}
				if !parsed["app.py"] || parsed["grow.py"] {
					t.Fatalf("parsed files = %v, want app.py parsed and grow.py skipped", parsed)
				}
				if !delta && !parsed["other.py"] {
					t.Fatalf("parsed files = %v, want other.py parsed in a full snapshot", parsed)
				}
				for _, meta := range got.ContentFileMetas {
					if meta.RelativePath == "grow.py" {
						t.Fatalf("ContentFileMetas carries grow.py; a skipped file must not reach the content stream")
					}
				}
				for _, paths := range got.ImportsMap {
					for _, path := range paths {
						if filepath.Base(path) == "grow.py" {
							t.Fatalf("ImportsMap names grow.py: %v", got.ImportsMap)
						}
					}
				}
				if delta {
					if len(got.DeletedRelativePaths) != 1 || got.DeletedRelativePaths[0] != "grow.py" {
						t.Fatalf("DeletedRelativePaths = %v, want [grow.py] so the delta retracts the grown target", got.DeletedRelativePaths)
					}
					return
				}
				if len(got.DeletedRelativePaths) != 0 {
					t.Fatalf("DeletedRelativePaths = %v, want none in a full snapshot", got.DeletedRelativePaths)
				}
			})
		}
	}
}

// TestPreScanGrowthSkipIsCountedLoggedAndDegrades (#8986 review P2): the
// pre-scan skip is recorded like the discovery oversize skip: one
// content:oversize count and one WARN with the relative path, size, and cap
// and no content or host path. The tree changed under the collector, so the
// snapshot is also marked degraded with file_read_failed, the same as growth
// before the parse read. Not parallel: it lowers the process-wide read cap and
// swaps gitCommitSHAFn.
func TestPreScanGrowthSkipIsCountedLoggedAndDegrades(t *testing.T) {
	defer shared.SetSourceReadCapForTest(oversizeTestCap)()

	repoRoot := writeGrowthRepo(t)
	defer growBetweenDiscoveryAndPreScan(t, filepath.Join(repoRoot, "grow.py"), oversizeTestBody)()
	source, logs, reader := dropRecordingSource(t)
	source.Snapshotter = NativeRepositorySnapshotter{Engine: defaultCollectorTestEngine(t), Logger: source.Logger, Instruments: source.Instruments}
	collected, err := source.snapshotOneRepository(context.Background(),
		SelectedRepository{RepoPath: repoRoot, PreparseIdentity: "preparse:v1:growth"}, "run", time.Now().UTC(), 0)
	if err != nil {
		t.Fatalf("snapshotOneRepository() error = %v", err)
	}
	drainFactChannel(collected.Facts)

	if counts := discoverySkipCounts(t, reader); counts["content:oversize"] != 1 {
		t.Fatalf("discovery skip counts = %v, want content:oversize=1", counts)
	}
	var lines []string
	for _, line := range strings.Split(logs.String(), "\n") {
		if strings.Contains(line, "msg=git_snapshot_file_skipped_oversize") {
			lines = append(lines, line)
		}
	}
	if len(lines) != 1 {
		t.Fatalf("oversize log lines = %d, want 1 (logs %s)", len(lines), logs.String())
	}
	for _, want := range []string{"level=WARN", "relative_path=grow.py", "size_bytes=" + strconv.Itoa(len(oversizeTestBody)), "cap_bytes=64"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("log %q lacks %q", lines[0], want)
		}
	}
	if strings.Contains(lines[0], "OVERSIZE-CONTENT-MARKER") || strings.Contains(lines[0], repoRoot) {
		t.Errorf("log %q carries file content or the host path", lines[0])
	}
	assertIdentityDropRecorded(t, logs.String(), dropCounts(t, reader), telemetry.PreparseDropFileReadFailed, repoRoot)
}

// TestPreScanStillFailsOnAnotherReadError: only a file over the read cap is a
// per-file skip. A file that vanished between discovery and pre-scan keeps
// failing the snapshot, as before. Not parallel: it swaps gitCommitSHAFn.
func TestPreScanStillFailsOnAnotherReadError(t *testing.T) {
	for _, workers := range []int{1, 4} {
		t.Run("workers="+strconv.Itoa(workers), func(t *testing.T) {
			repoRoot := writeGrowthRepo(t)
			path := filepath.Join(repoRoot, "grow.py")
			original := gitCommitSHAFn
			gitCommitSHAFn = func(context.Context, string) string {
				if err := os.Remove(path); err != nil {
					t.Errorf("remove %s: %v", path, err)
				}
				return "c"
			}
			defer func() { gitCommitSHAFn = original }()
			snapshotter := NativeRepositorySnapshotter{Engine: defaultCollectorTestEngine(t), ParseWorkers: workers}
			if _, err := snapshotter.SnapshotRepository(context.Background(), SelectedRepository{RepoPath: repoRoot}); err == nil {
				t.Fatal("SnapshotRepository() error = nil, want a pre-scan failure for a file that vanished")
			}
		})
	}
}
