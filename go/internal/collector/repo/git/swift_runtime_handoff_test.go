// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSwiftRuntimeNativeHandoff pins the read-once Package.swift handoff
// (#9308 Others lane ii): the native parse carries the tools-version evidence
// through the snapshot once, the declaration emits through the distinct
// swift_tools_version kind, and the private evidence never persists.
func TestSwiftRuntimeNativeHandoff(t *testing.T) {
	root := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(root, "Package.swift"), "// swift-tools-version: 5.9\nimport PackageDescription\n")
	observed := time.Date(2026, time.October, 30, 0, 0, 0, 0, time.UTC)
	snapshot, err := (NativeRepositorySnapshotter{Now: func() time.Time { return observed }}).SnapshotRepository(context.Background(), SelectedRepository{RepoPath: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.SwiftRuntime) != 1 {
		t.Fatalf("Swift handoffs = %d, want 1", len(snapshot.SwiftRuntime))
	}
	for _, parsed := range snapshot.FileData {
		if _, ok := parsed["swift_runtime_evidence"]; ok {
			t.Fatal("private Swift evidence persisted")
		}
	}
	collected := buildStreamingGeneration(root, testCollectorRepositoryMetadata(root), "run-swift", observed, snapshot, false, "")
	declarations := decodeDeclarationList(t, drainFactChannel(collected.Facts))
	if len(declarations) != 1 || declarations[0].SourceKind != "swift_tools_version" || declarations[0].Runtime != "swift" || declarations[0].RawSpec != "5.9" || declarations[0].LineNumber != 1 {
		t.Fatalf("Swift declarations = %+v", declarations)
	}
}

// TestSwiftRuntimeRejectsCredentialEvidence pins the credential guard before
// emission: a credential-shaped tools version emits nothing and neither the
// private evidence nor the credential reaches any fact.
func TestSwiftRuntimeRejectsCredentialEvidence(t *testing.T) {
	root := t.TempDir()
	seed := "swift-seed-9f2c4b7a"
	writeCollectorTestFile(t, filepath.Join(root, "Package.swift"), "// swift-tools-version: https://user:"+seed+"@example.invalid/5.9\n")
	snapshot, err := (NativeRepositorySnapshotter{}).SnapshotRepository(context.Background(), SelectedRepository{RepoPath: root})
	if err != nil {
		t.Fatal(err)
	}
	collected := buildStreamingGeneration(root, testCollectorRepositoryMetadata(root), "run-swift-guard", time.Now(), snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)
	for _, envelope := range envelopes {
		body, err := json.Marshal(envelope.Payload)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "swift_runtime_evidence") {
			t.Fatal("private Swift evidence persisted")
		}
	}
	for _, row := range decodeDeclarationList(t, envelopes) {
		if row.SourceKind == "swift_tools_version" {
			t.Fatalf("credential-shaped tools version emitted: %+v", row)
		}
		if strings.Contains(row.RawSpec, seed) {
			t.Fatalf("credential reached a declaration: %+v", row)
		}
	}
}
