// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// oversizeTestCap is the lowered read cap these tests run under, so a file of
// a few hundred bytes stands in for one over the 64 MiB production cap.
const oversizeTestCap = 64

// oversizeTestBody is a valid Python source over oversizeTestCap bytes. Its
// marker string must never appear in a log line.
var oversizeTestBody = "def big():\n    return 'OVERSIZE-CONTENT-MARKER'\n" + strings.Repeat("# padding line\n", 12)

// writeOversizeRepo lays out a repository with one small parser file, one
// parser file over the cap, a tfstate candidate over the cap (metadata-only,
// never read whole), and a documentation file over the cap (read through its
// own bounded reader).
func writeOversizeRepo(t *testing.T) string {
	t.Helper()
	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, "app.py"), "def main():\n    return 1\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "big.py"), oversizeTestBody)
	writeCollectorTestFile(t, filepath.Join(repoRoot, "state", "prod.tfstate"), "{\"version\": 4}\n"+strings.Repeat(" ", 200))
	writeCollectorTestFile(t, filepath.Join(repoRoot, "docs", "guide.md"), "# Guide\n\n"+strings.Repeat("Some guide text.\n", 20))
	return repoRoot
}

// discoverySkipCounts reads eshu_dp_discovery_files_skipped_total by skip_reason.
func discoverySkipCounts(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("collect: %v", err)
	}
	out := map[string]int64{}
	for _, scopeMetrics := range collected.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != "eshu_dp_discovery_files_skipped_total" {
				continue
			}
			for _, point := range m.Data.(metricdata.Sum[int64]).DataPoints {
				value, _ := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionSkipReason))
				out[value.AsString()] += point.Value
			}
		}
	}
	return out
}

// TestSnapshotRepositorySkipsParserFilesOverTheReadCap (#8986): a parser file
// whose discovered size is over the read cap never reaches the parse or the
// content stream, the rest of the repository keeps indexing, and the skip is
// recorded with its relative path and size. tfstate candidates and
// documentation files are not parser reads and keep their own handling. Not
// parallel: it lowers the process-wide read cap.
func TestSnapshotRepositorySkipsParserFilesOverTheReadCap(t *testing.T) {
	defer shared.SetSourceReadCapForTest(oversizeTestCap)()

	for _, delta := range []bool{false, true} {
		name := "full"
		if delta {
			name = "delta"
		}
		t.Run(name, func(t *testing.T) {
			repoRoot := writeOversizeRepo(t)
			repository := SelectedRepository{RepoPath: repoRoot}
			if delta {
				repository.FileTargets = []string{filepath.Join(repoRoot, "app.py"), filepath.Join(repoRoot, "big.py")}
				repository.Delta = true
			}
			snapshotter := NativeRepositorySnapshotter{Engine: defaultCollectorTestEngine(t)}
			got, err := snapshotter.SnapshotRepository(context.Background(), repository)
			if err != nil {
				t.Fatalf("SnapshotRepository() error = %v", err)
			}

			want := []SkippedOversizeFile{{RelativePath: "big.py", SizeBytes: int64(len(oversizeTestBody))}}
			if len(got.OversizeSkippedFiles) != 1 || got.OversizeSkippedFiles[0] != want[0] {
				t.Fatalf("OversizeSkippedFiles = %#v, want %#v", got.OversizeSkippedFiles, want)
			}
			parsed := map[string]bool{}
			for _, fileData := range got.FileData {
				parsed[filepath.Base(model.PayloadPath(fileData, "path"))] = true
			}
			if !parsed["app.py"] || parsed["big.py"] {
				t.Fatalf("parsed files = %v, want app.py parsed and big.py skipped", parsed)
			}
			for _, meta := range got.ContentFileMetas {
				if meta.RelativePath == "big.py" {
					t.Fatalf("ContentFileMetas carries big.py; an oversize file must not reach the content stream")
				}
			}
			if delta {
				return
			}
			if len(got.TerraformStateCandidates) != 1 || got.TerraformStateCandidates[0].RelativePath != "state/prod.tfstate" {
				t.Fatalf("TerraformStateCandidates = %#v, want the oversize tfstate candidate kept", got.TerraformStateCandidates)
			}
			docs := false
			for _, meta := range got.DocumentationFileMetas {
				docs = docs || meta.RelativePath == "docs/guide.md"
			}
			if !docs {
				t.Fatalf("DocumentationFileMetas = %#v, want docs/guide.md kept", got.DocumentationFileMetas)
			}
		})
	}
}

// TestSnapshotOneRepositoryReportsOversizeSkip (#8986): the skip is counted as
// skip_reason=content:oversize and logged WARN once per file with the
// repository, scope, and generation ids, the relative path, the size, and the
// cap, and never the file content or the host path. A size skip is
// deterministic for the commit, so it keeps the pre-parse identity: it is not
// a degraded snapshot. Not parallel: it lowers the process-wide read cap.
func TestSnapshotOneRepositoryReportsOversizeSkip(t *testing.T) {
	defer shared.SetSourceReadCapForTest(oversizeTestCap)()

	repoRoot := writeOversizeRepo(t)
	source, logs, reader := dropRecordingSource(t)
	source.Snapshotter = NativeRepositorySnapshotter{Engine: defaultCollectorTestEngine(t), Logger: source.Logger, Instruments: source.Instruments}
	const identity = "preparse:v1:oversize"
	collected, err := source.snapshotOneRepository(context.Background(),
		SelectedRepository{RepoPath: repoRoot, PreparseIdentity: identity}, "run", time.Now().UTC(), 0)
	if err != nil {
		t.Fatalf("snapshotOneRepository() error = %v", err)
	}
	drainFactChannel(collected.Facts)

	if got := collected.Generation.PreparseIdentity; got != identity {
		t.Fatalf("generation identity = %q, want %q (a size skip is not a degradation)", got, identity)
	}
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
	for _, want := range []string{
		"level=WARN", "repository_id=repository:", "scope_id=git-repository-scope:", "generation_id=",
		"relative_path=big.py", "size_bytes=" + strconv.Itoa(len(oversizeTestBody)), "cap_bytes=64",
	} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("log %q lacks %q", lines[0], want)
		}
	}
	if strings.Contains(lines[0], "OVERSIZE-CONTENT-MARKER") || strings.Contains(lines[0], repoRoot) {
		t.Errorf("log %q carries file content or the host path", lines[0])
	}
	assertIdentityDropRecorded(t, logs.String(), dropCounts(t, reader), "", repoRoot)
}

// TestParseRepositoryFileRefusesAFileThatGrewPastTheCap (#8986): discovery
// sized the file under the cap but it grew before the parse read. The parse
// read is capped too, so the file is skipped instead of read whole, and the
// snapshot is marked degraded because the tree changed under the collector.
// Not parallel: it lowers the process-wide read cap.
func TestParseRepositoryFileRefusesAFileThatGrewPastTheCap(t *testing.T) {
	defer shared.SetSourceReadCapForTest(oversizeTestCap)()

	repoRoot := t.TempDir()
	path := filepath.Join(repoRoot, "big.py")
	writeCollectorTestFile(t, path, oversizeTestBody)
	ctx, degradation := withSnapshotDegradation(context.Background())
	snapshotter := NativeRepositorySnapshotter{Engine: defaultCollectorTestEngine(t)}
	engine, err := snapshotter.engine()
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	result := snapshotter.parseRepositoryFile(ctx, repoRoot, parseFileJob{index: 0, path: path}, engine, "c", false, nil, "repository:r_test", nil)
	if !result.skipped {
		t.Fatalf("parseRepositoryFile() skipped = false, want true for a file over the cap")
	}
	if reason, degraded := degradation.degraded(); !degraded || reason != telemetry.PreparseDropFileReadFailed {
		t.Fatalf("degradation = (%q, %v), want (%q, true)", reason, degraded, telemetry.PreparseDropFileReadFailed)
	}
}

// TestStreamRefusesAContentFileThatGrewPastTheCap (#8986): the fact stream's
// content read is capped, so a content file that grew past the cap between the
// parse and the emit is dropped as content_read_failed, not read whole. Not
// parallel: it lowers the process-wide read cap.
func TestStreamRefusesAContentFileThatGrewPastTheCap(t *testing.T) {
	defer shared.SetSourceReadCapForTest(oversizeTestCap)()

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, "big.py"), oversizeTestBody)
	snapshot := RepositorySnapshot{RepoPath: repoRoot, HeadCommitSHA: "c", PreparseIdentity: "preparse:v1:x"}
	snapshot.ContentFileMetas = []model.ContentFileMeta{{RelativePath: "big.py", Digest: "d", Language: "python", CommitSHA: "c"}}
	source, logs, reader := dropRecordingSource(t)
	collected := buildStreamingGenerationWithContext(source.withStreamDropRecorder(context.Background()),
		repoRoot, testCollectorRepositoryMetadata(repoRoot), "run", time.Now().UTC(), snapshot, false, "")
	for _, envelope := range drainFactChannel(collected.Facts) {
		if strings.Contains(envelope.FactKind, "content") && strings.Contains(fmt.Sprint(envelope.Payload), "OVERSIZE-CONTENT-MARKER") {
			t.Fatalf("fact %q carries the oversize body", envelope.FactKind)
		}
	}
	assertIdentityDropRecorded(t, logs.String(), dropCounts(t, reader), telemetry.PreparseDropContentReadFailed, repoRoot)
}
