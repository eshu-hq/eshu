// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"bytes"
	"context"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// dropRecordingSource is a GitSource that logs to a buffer and counts on a
// manual metric reader, so a test can read the identity-drop WARN and counter.
func dropRecordingSource(t *testing.T) (*GitSource, *bytes.Buffer, *sdkmetric.ManualReader) {
	t.Helper()
	var logs bytes.Buffer
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("t"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	return &GitSource{Component: "test", Logger: slog.New(slog.NewTextHandler(&logs, nil)), Instruments: inst}, &logs, reader
}

// dropCounts reads eshu_dp_collector_preparse_identity_dropped_total by reason.
func dropCounts(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("collect: %v", err)
	}
	out := map[string]int64{}
	for _, scopeMetrics := range collected.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != "eshu_dp_collector_preparse_identity_dropped_total" {
				continue
			}
			for _, point := range m.Data.(metricdata.Sum[int64]).DataPoints {
				value, _ := point.Attributes.Value(attribute.Key("reason"))
				out[value.AsString()] += point.Value
			}
		}
	}
	return out
}

// assertIdentityDropRecorded checks one drop by reason: the counter moved by
// one for reason, and the WARN carries every id and no host path. An empty
// reason asserts that nothing was recorded.
func assertIdentityDropRecorded(t *testing.T, logs string, counts map[string]int64, reason telemetry.PreparseIdentityDropReason, hostRoot string) {
	t.Helper()
	if reason == "" {
		if len(counts) != 0 || strings.Contains(logs, "git_snapshot_preparse_identity_dropped") {
			t.Fatalf("drop recorded with no drop: counts %v, logs %s", counts, logs)
		}
		return
	}
	if counts[string(reason)] != 1 || len(counts) != 1 {
		t.Errorf("drop counts = %v, want only %s=1", counts, reason)
	}
	var dropLines []string
	for _, line := range strings.Split(logs, "\n") {
		if strings.Contains(line, "msg=git_snapshot_preparse_identity_dropped") {
			dropLines = append(dropLines, line)
		}
	}
	if len(dropLines) != 1 {
		t.Fatalf("drop log lines = %d, want 1 (logs %s)", len(dropLines), logs)
	}
	logs = dropLines[0]
	for _, want := range []string{"level=WARN", "msg=git_snapshot_preparse_identity_dropped", "reason=" + string(reason), "repository_id=repository:", "scope_id=git-repository-scope:", "generation_id="} {
		if !strings.Contains(logs, want) {
			t.Errorf("log %q lacks %q", logs, want)
		}
	}
	if strings.Contains(logs, hostRoot) || strings.Contains(logs, "repo_path=") {
		t.Errorf("log %q carries a host path", logs)
	}
}

// TestStreamDropMarksTheGeneration (#7978 review F-7, F7-1, ruling a9-143.19):
// when the fact stream cannot read a content, workflow, or documentation file
// between the parse and the emit, it skips the file, sets the generation's
// PreparseIdentityDropped flag, logs WARN with the relative path, and counts
// the drop by reason. An unsupported documentation format is deterministic and
// drops nothing. The store side is TestPreparseIdentityDroppedByTheStreamLive.
func TestStreamDropMarksTheGeneration(t *testing.T) {
	for _, cell := range []struct {
		name       string
		path       string
		noIdentity bool
		reason     telemetry.PreparseIdentityDropReason
		set        func(*RepositorySnapshot, model.ContentFileMeta)
	}{
		// Review F7-5: a generation without a pre-parse identity is marked
		// too, so the store can keep the next equal snapshot from re-opening it.
		{name: "content file, no identity", path: "app.py", noIdentity: true, reason: telemetry.PreparseDropContentReadFailed, set: func(s *RepositorySnapshot, m model.ContentFileMeta) {
			s.ContentFileMetas = []model.ContentFileMeta{m}
		}},
		{name: "content file", path: "app.py", reason: telemetry.PreparseDropContentReadFailed, set: func(s *RepositorySnapshot, m model.ContentFileMeta) {
			s.ContentFileMetas = []model.ContentFileMeta{m}
		}},
		{name: "workflow file", path: ".github/workflows/ci.yml", reason: telemetry.PreparseDropWorkflowReadFailed, set: func(s *RepositorySnapshot, m model.ContentFileMeta) {
			s.WorkflowImageFileMetas = []model.ContentFileMeta{m}
		}},
		{name: "documentation file", path: "docs/guide.md", reason: telemetry.PreparseDropDocsBodyReadFailed, set: func(s *RepositorySnapshot, m model.ContentFileMeta) {
			s.DocumentationFileMetas = []model.ContentFileMeta{m}
		}},
		{name: "unsupported documentation format", path: "docs/blob.unsupported", set: func(s *RepositorySnapshot, m model.ContentFileMeta) {
			s.DocumentationFileMetas = []model.ContentFileMeta{m}
		}},
	} {
		t.Run(cell.name, func(t *testing.T) {
			repoRoot := t.TempDir() // no file exists on disk: every body read fails
			snapshot := RepositorySnapshot{RepoPath: repoRoot, HeadCommitSHA: "c", PreparseIdentity: "preparse:v1:x"}
			if cell.noIdentity {
				snapshot.PreparseIdentity = ""
			}
			cell.set(&snapshot, model.ContentFileMeta{RelativePath: cell.path, Digest: "d", Language: "python", CommitSHA: "c"})
			source, logs, reader := dropRecordingSource(t)
			collected := buildStreamingGenerationWithContext(source.withStreamDropRecorder(context.Background()),
				repoRoot, testCollectorRepositoryMetadata(repoRoot), "run", time.Now().UTC(), snapshot, false, "")
			drainFactChannel(collected.Facts)
			dropped := collected.Generation.PreparseIdentityDropped != nil && collected.Generation.PreparseIdentityDropped.Load()
			if dropped != (cell.reason != "") {
				t.Fatalf("PreparseIdentityDropped = %v, want %v (hook false after a drop)", dropped, cell.reason != "")
			}
			assertIdentityDropRecorded(t, logs.String(), dropCounts(t, reader), cell.reason, repoRoot)
			if cell.reason != "" && !strings.Contains(logs.String(), "relative_path="+cell.path) {
				t.Errorf("log %q lacks the relative path %s", logs.String(), cell.path)
			}
		})
	}
}

// TestSnapshotPhaseReadFailuresMarkTheSnapshot (#7978 review F7-1, ruling
// a9-143.19): the documentation, CODEOWNERS, and workflow-image digest reads
// run before the parse; a file whose digest cannot be read is skipped, and the
// caller marks the snapshot degraded with its reason, so the snapshot stamps no
// pre-parse identity.
func TestSnapshotPhaseReadFailuresMarkTheSnapshot(t *testing.T) {
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine: %v", err)
	}
	for _, cell := range []struct {
		name, locked string
		delta        bool
		reason       telemetry.PreparseIdentityDropReason
	}{
		{name: "documentation digest", locked: "docs/guide.md", reason: telemetry.PreparseDropDocsReadFailed},
		{name: "codeowners digest", locked: ".github/CODEOWNERS", reason: telemetry.PreparseDropCodeownersReadFailed},
		{name: "workflow image digest", locked: ".github/workflows/ci.yml", delta: true, reason: telemetry.PreparseDropWorkflowDigestFailed},
	} {
		t.Run(cell.name, func(t *testing.T) {
			repoRoot := t.TempDir()
			writeCollectorTestFile(t, filepath.Join(repoRoot, "app.py"), "def main():\n    return 1\n")
			locked := filepath.Join(repoRoot, filepath.FromSlash(cell.locked))
			writeCollectorTestFile(t, locked, "jobs: {}\n")
			if err := os.Chmod(locked, 0); err != nil {
				t.Fatalf("chmod: %v", err)
			}
			t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })
			if _, err := os.ReadFile(locked); err == nil {
				t.Skip("this user can read a mode-0 file (root?); the read-failure cell cannot run")
			}
			repository := SelectedRepository{RepoPath: repoRoot}
			if cell.delta {
				repository.Delta = true
				repository.FileTargets = []string{filepath.Join(repoRoot, "app.py")}
				repository.DeltaBaselineCommitSHA = "A"
			}
			ctx, note := withSnapshotDegradation(context.Background())
			if _, err := (NativeRepositorySnapshotter{Engine: engine}).SnapshotRepository(ctx, repository); err != nil {
				t.Fatalf("SnapshotRepository: %v", err)
			}
			if reason, degraded := note.degraded(); !degraded || reason != cell.reason {
				t.Fatalf("snapshot degradation = %q/%v, want %q", reason, degraded, cell.reason)
			}
		})
	}
}

// TestRecordIdentityDropNormalizesUnknownReason (review F7-2): a reason
// outside the closed set is counted as "other", so the label stays bounded.
func TestRecordIdentityDropNormalizesUnknownReason(t *testing.T) {
	source, _, reader := dropRecordingSource(t)
	source.recordIdentityDrop(context.Background(), identityDrop{Reason: "not_a_reason"})
	if counts := dropCounts(t, reader); counts["other"] != 1 || len(counts) != 1 {
		t.Fatalf("drop counts = %v, want only other=1", counts)
	}
}

// TestDropReasonsAreClosed (review F7-2, ruling a9-143.19): every
// PreparseIdentityDropReason constant in the telemetry registry is in
// PreparseIdentityDropReasons(), and every drop site in this package passes a
// registry constant (telemetry.PreparseDrop...) or a value already typed by
// one, never a string literal. A new reason without a registry row, or a
// literal at a call site, fails here.
func TestDropReasonsAreClosed(t *testing.T) {
	t.Parallel()
	registry := map[string]bool{}
	for _, reason := range telemetry.PreparseIdentityDropReasons() {
		registry[string(reason)] = true
	}
	declared := declaredDropReasonConsts(t, filepath.Join("..", "..", "..", "telemetry", "instruments_reconcile_parse_skip.go"))
	for name, value := range declared {
		if value != string(telemetry.PreparseDropOther) && !registry[value] {
			t.Errorf("reason constant %s = %q is not in PreparseIdentityDropReasons()", name, value)
		}
	}
	if len(declared) != len(registry)+1 {
		t.Errorf("declared %d reason constants, registry lists %d plus the fallback", len(declared), len(registry))
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	sites := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := goparser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			var reason ast.Expr
			switch n := node.(type) {
			case *ast.CallExpr:
				if id, ok := n.Fun.(*ast.Ident); ok && (id.Name == "markSnapshotDegraded" || id.Name == "markSnapshotDegradedOn") {
					reason = n.Args[len(n.Args)-1]
				}
			case *ast.CompositeLit:
				if typ, ok := n.Type.(*ast.Ident); ok && typ.Name == "identityDrop" {
					for _, elt := range n.Elts {
						if kv, ok := elt.(*ast.KeyValueExpr); ok {
							if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Reason" {
								reason = kv.Value
							}
						}
					}
				}
			}
			if reason == nil {
				return true
			}
			sites++
			switch r := reason.(type) {
			case *ast.SelectorExpr:
				pkg, ok := r.X.(*ast.Ident)
				if !ok || pkg.Name != "telemetry" || declared[r.Sel.Name] == "" {
					t.Errorf("%s: drop reason %s is not a registry constant", file, r.Sel.Name)
				}
			case *ast.BasicLit:
				t.Errorf("%s: drop reason %s is a literal, not a registry constant", file, r.Value)
			}
			return true
		})
	}
	if sites < 10 {
		t.Fatalf("found %d drop sites, want at least 10; the scan no longer sees them", sites)
	}
}

// declaredDropReasonConsts parses the telemetry registry file and returns the
// PreparseIdentityDropReason constants by name.
func declaredDropReasonConsts(t *testing.T, path string) map[string]string {
	t.Helper()
	parsed, err := goparser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	out := map[string]string{}
	for _, decl := range parsed.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			value := spec.(*ast.ValueSpec)
			if typ, ok := value.Type.(*ast.Ident); !ok || typ.Name != "PreparseIdentityDropReason" {
				continue
			}
			for i, name := range value.Names {
				if lit, ok := value.Values[i].(*ast.BasicLit); ok {
					out[name.Name] = strings.Trim(lit.Value, `"`)
				}
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("no PreparseIdentityDropReason constants in %s", path)
	}
	return out
}
