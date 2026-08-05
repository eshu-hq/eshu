// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

func TestNativeRepositorySnapshotterCarriesDeletedOnlyDeltaMetadata(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := (NativeRepositorySnapshotter{Engine: engine}).SnapshotRepository(
		context.Background(),
		SelectedRepository{
			RepoPath:             repoRoot,
			Delta:                true,
			DeletedRelativePaths: []string{"old/deleted.go"},
		},
	)
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v, want nil", err)
	}
	if !got.Delta {
		t.Fatal("Delta = false, want true")
	}
	if got.FileCount != 0 {
		t.Fatalf("FileCount = %d, want 0", got.FileCount)
	}
	if got, want := got.DeltaRelativePaths, []string{"old/deleted.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DeltaRelativePaths = %#v, want %#v", got, want)
	}
	if got, want := got.DeletedRelativePaths, []string{"old/deleted.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DeletedRelativePaths = %#v, want %#v", got, want)
	}
}

func TestNativeRepositorySnapshotterDeltaTargetsKeepFullPreScanContext(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	targetFile := filepath.Join(repoRoot, "app.py")
	writeCollectorTestFile(
		t,
		targetFile,
		"from helpers import Helper\n\ndef handler():\n    return Helper()\n",
	)
	writeCollectorTestFile(
		t,
		filepath.Join(repoRoot, "helpers.py"),
		"class Helper:\n    pass\n",
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := (NativeRepositorySnapshotter{Engine: engine}).SnapshotRepository(
		context.Background(),
		SelectedRepository{
			RepoPath:    repoRoot,
			FileTargets: []string{targetFile},
			Delta:       true,
		},
	)
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v, want nil", err)
	}

	if got, want := got.FileCount, 1; got != want {
		t.Fatalf("FileCount = %d, want %d", got, want)
	}
	if got, want := len(got.FileData), 1; got != want {
		t.Fatalf("len(FileData) = %d, want %d", got, want)
	}
	if got, want := len(got.ContentFileMetas), 1; got != want {
		t.Fatalf("len(ContentFileMetas) = %d, want %d", got, want)
	}
	helperPaths, ok := got.ImportsMap["Helper"]
	if !ok {
		t.Fatalf("ImportsMap missing unchanged Helper entry: %#v", got.ImportsMap)
	}
	if got, want := filepath.Base(helperPaths[0]), "helpers.py"; got != want {
		t.Fatalf("ImportsMap[Helper][0] base = %q, want %q", got, want)
	}
}

func TestNativeRepositorySnapshotterPreservesDeltaMetadataPathWhitespace(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	targetFile := filepath.Join(repoRoot, "dir", " file.go")
	writeCollectorTestFile(t, targetFile, "package dir\n")

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := (NativeRepositorySnapshotter{Engine: engine}).SnapshotRepository(
		context.Background(),
		SelectedRepository{
			RepoPath:             repoRoot,
			FileTargets:          []string{targetFile},
			Delta:                true,
			DeletedRelativePaths: []string{"dir/deleted .go"},
		},
	)
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v, want nil", err)
	}

	if got, want := got.DeltaRelativePaths, []string{"dir/ file.go", "dir/deleted .go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DeltaRelativePaths = %#v, want %#v", got, want)
	}
	if got, want := got.DeletedRelativePaths, []string{"dir/deleted .go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DeletedRelativePaths = %#v, want %#v", got, want)
	}
}

func TestNativeRepositorySnapshotterDeltaCarriesCurrentWorkflowImageSnapshot(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	changedFile := filepath.Join(repoRoot, "app.go")
	workflowPath := filepath.Join(repoRoot, ".github", "workflows", "deploy.yml")
	writeCollectorTestFile(t, changedFile, "package app\n")
	writeCollectorTestFile(t, workflowPath, workflowImageDeltaFixture("ghcr.io/acme/api:v1"))

	snapshot := snapshotDeltaForWorkflowImageTest(t, repoRoot, []string{changedFile}, nil, "commit-unrelated")
	if got, want := snapshot.FileCount, 1; got != want {
		t.Fatalf("FileCount = %d, want %d (ordinary parsing must stay delta-narrow)", got, want)
	}
	if got, want := snapshot.DeltaRelativePaths, []string{"app.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DeltaRelativePaths = %#v, want original delta paths %#v", got, want)
	}
	if got, want := len(snapshot.WorkflowImageFileMetas), 1; got != want {
		t.Fatalf("len(WorkflowImageFileMetas) = %d, want %d", got, want)
	}
	if got, want := snapshot.WorkflowImageFileMetas[0].RelativePath, ".github/workflows/deploy.yml"; got != want {
		t.Fatalf("WorkflowImageFileMetas[0].RelativePath = %q, want %q", got, want)
	}

	payloads := workflowImagePayloadsForSnapshot(t, repoRoot, snapshot)
	if len(payloads) == 0 {
		t.Fatal("unrelated delta emitted no ci.workflow_image_evidence; want unchanged workflow retained")
	}
	for _, payload := range payloads {
		if got, want := payload["image_ref"], "ghcr.io/acme/api:v1"; got != want {
			t.Fatalf("image_ref = %#v, want %#v", got, want)
		}
		if got, want := payload["commit_sha"], "commit-unrelated"; got != want {
			t.Fatalf("commit_sha = %#v, want %#v", got, want)
		}
	}
}

func TestNativeRepositorySnapshotterDeltaChangedWorkflowReplacesEvidence(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	workflowPath := filepath.Join(repoRoot, ".github", "workflows", "deploy.yaml")
	writeCollectorTestFile(t, workflowPath, workflowImageDeltaFixture("ghcr.io/acme/api:v2"))

	snapshot := snapshotDeltaForWorkflowImageTest(t, repoRoot, []string{workflowPath}, nil, "commit-changed")
	if got := len(snapshot.WorkflowImageFileMetas); got != 0 {
		t.Fatalf("len(WorkflowImageFileMetas) = %d, want 0 for already-targeted workflow", got)
	}
	payloads := workflowImagePayloadsForSnapshot(t, repoRoot, snapshot)
	if len(payloads) == 0 {
		t.Fatal("changed workflow emitted no ci.workflow_image_evidence")
	}
	for _, payload := range payloads {
		if got, want := payload["image_ref"], "ghcr.io/acme/api:v2"; got != want {
			t.Fatalf("image_ref = %#v, want current value %#v", got, want)
		}
		if got, want := payload["commit_sha"], "commit-changed"; got != want {
			t.Fatalf("commit_sha = %#v, want current commit %#v", got, want)
		}
	}
}

func TestNativeRepositorySnapshotterDeltaDeletedWorkflowRemovesEvidence(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	changedFile := filepath.Join(repoRoot, "app.go")
	writeCollectorTestFile(t, changedFile, "package app\n")

	snapshot := snapshotDeltaForWorkflowImageTest(
		t,
		repoRoot,
		[]string{changedFile},
		[]string{".github/workflows/deploy.yml"},
		"commit-deleted",
	)
	if got, want := snapshot.DeltaRelativePaths, []string{".github/workflows/deploy.yml", "app.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DeltaRelativePaths = %#v, want %#v", got, want)
	}
	if got := len(snapshot.WorkflowImageFileMetas); got != 0 {
		t.Fatalf("len(WorkflowImageFileMetas) = %d, want 0 after workflow deletion", got)
	}
	if payloads := workflowImagePayloadsForSnapshot(t, repoRoot, snapshot); len(payloads) != 0 {
		t.Fatalf("deleted workflow emitted stale ci.workflow_image_evidence: %#v", payloads)
	}
}

func snapshotDeltaForWorkflowImageTest(
	t *testing.T,
	repoRoot string,
	fileTargets []string,
	deletedRelativePaths []string,
	commitSHA string,
) RepositorySnapshot {
	t.Helper()
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	snapshot, err := (NativeRepositorySnapshotter{Engine: engine}).SnapshotRepository(
		context.Background(),
		SelectedRepository{
			RepoPath:             repoRoot,
			FileTargets:          fileTargets,
			Delta:                true,
			DeletedRelativePaths: deletedRelativePaths,
			SourceCommitSHA:      commitSHA,
		},
	)
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v, want nil", err)
	}
	return snapshot
}

func workflowImagePayloadsForSnapshot(t *testing.T, repoRoot string, snapshot RepositorySnapshot) []map[string]any {
	t.Helper()
	collected := buildStreamingGeneration(
		repoRoot,
		repositoryidentity.Metadata{ID: "repository:test-workflow-delta", Name: "workflow-delta"},
		"workflow-delta-run",
		time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC),
		snapshot,
		false,
		"",
	)
	var payloads []map[string]any
	for envelope := range collected.Facts {
		if envelope.FactKind == facts.CICDWorkflowImageEvidenceFactKind {
			payloads = append(payloads, envelope.Payload)
		}
	}
	return payloads
}

func workflowImageDeltaFixture(imageRef string) string {
	return strings.Join([]string{
		"name: deploy",
		"jobs:",
		"  build:",
		"    runs-on: ubuntu-latest",
		"    steps:",
		"      - name: Push image",
		"        run: docker push " + imageRef,
		"",
	}, "\n")
}
