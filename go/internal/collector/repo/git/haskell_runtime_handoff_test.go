// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	gitruntime "github.com/eshu-hq/eshu/go/internal/collector/repo/git/runtime/declaration"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestHaskellRuntimeNativeHandoff(t *testing.T) {
	root := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(root, "stack.yaml"), "packages:\n  - .\nresolver: lts-22.43\nextra-deps: []\n")
	observed := time.Date(2026, time.October, 30, 0, 0, 0, 0, time.UTC)
	snapshot, err := (NativeRepositorySnapshotter{Now: func() time.Time { return observed }}).SnapshotRepository(context.Background(), SelectedRepository{RepoPath: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.HaskellRuntime) != 1 {
		t.Fatalf("Haskell handoffs = %d, want 1", len(snapshot.HaskellRuntime))
	}
	for _, parsed := range snapshot.FileData {
		if _, ok := parsed["haskell_runtime_evidence"]; ok {
			t.Fatal("private Haskell evidence persisted")
		}
	}
	collected := buildStreamingGeneration(root, testCollectorRepositoryMetadata(root), "run-haskell", observed, snapshot, false, "")
	declarations := decodeDeclarationList(t, drainFactChannel(collected.Facts))
	if len(declarations) != 1 || declarations[0].SourceKind != "stack_yaml_resolver" || declarations[0].Runtime != "haskell" || declarations[0].RawSpec != "lts-22.43" || declarations[0].LineNumber != 3 {
		t.Fatalf("Haskell declarations = %+v", declarations)
	}
}

// TestStackYamlCloudFormationCandidateFallsThrough mirrors the parser-level
// TestStackYamlCloudFormationFallsThrough at candidate dispatch: a
// CloudFormation template literally named stack.yaml carries no Stack
// evidence, so the Haskell candidate must fall through to generic handling
// with no dropped mark and no Unavailable candidate.
func TestStackYamlCloudFormationCandidateFallsThrough(t *testing.T) {
	body := "AWSTemplateFormatVersion: '2010-09-09'\nResources:\n  Logs:\n    Type: AWS::S3::Bucket\n"
	for _, relativePath := range []string{"stack.yaml", "infra/stack.yaml"} {
		snapshot := &RepositorySnapshot{HaskellRuntimeExpected: true}
		candidates := make(map[string]gitruntime.Candidate)
		var dropped atomic.Bool
		droppedPaths := &scope.StreamDroppedPaths{}
		noteComposerOrRuntimeCandidate(snapshot, candidates, relativePath, body, &dropped, droppedPaths)
		if dropped.Load() {
			t.Fatalf("%s: CloudFormation body marked stream dropped", relativePath)
		}
		if paths := droppedPaths.Sorted(); len(paths) != 0 {
			t.Fatalf("%s: dropped paths = %v, want none", relativePath, paths)
		}
		if len(candidates) != 0 {
			t.Fatalf("%s: candidates = %+v, want none", relativePath, candidates)
		}
	}
}
