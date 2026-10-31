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

	runtimedeclaration "github.com/eshu-hq/eshu/go/internal/collector/repo/runtime/declaration"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestDartRuntimeNativeHandoff(t *testing.T) {
	root := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(root, "pubspec.yaml"), "name: demo\nenvironment:\n  sdk: '>=3.0.0 <4.0.0'\n")
	writeCollectorTestFile(t, filepath.Join(root, ".fvmrc"), "{\n  \"flutter\": \"3.22.0\"\n}\n")
	observed := time.Date(2026, time.October, 29, 0, 0, 0, 0, time.UTC)
	snapshot, err := (NativeRepositorySnapshotter{Now: func() time.Time { return observed }}).SnapshotRepository(context.Background(), SelectedRepository{RepoPath: root})
	if err != nil {
		t.Fatal(err)
	}
	collected := buildStreamingGeneration(root, testCollectorRepositoryMetadata(root), "run-dart", observed, snapshot, false, "")
	got := decodeDeclarationList(t, drainFactChannel(collected.Facts))
	if len(got) != 2 {
		t.Fatalf("Dart declarations = %+v, want two", got)
	}
	want := map[string]bool{
		"dart_pubspec_environment_sdk:>=3.0.0 <4.0.0": true,
		"dart_fvmrc_flutter:3.22.0":                   true,
	}
	for _, row := range got {
		key := row.SourceKind + ":" + row.RawSpec
		if !want[key] || row.Runtime != "dart" {
			t.Errorf("declaration = %+v", row)
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Fatalf("missing Dart declarations: %+v", want)
	}
}

func TestDartRuntimeRejectsTransportInPersistedFacts(t *testing.T) {
	root := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(root, "pubspec.yaml"), "name: demo\nenvironment:\n  sdk: '${DART_SDK}'\n")
	writeCollectorTestFile(t, filepath.Join(root, ".fvmrc"), `{"flutter":"https://user:password@example.invalid"}`)
	snapshot, err := (NativeRepositorySnapshotter{}).SnapshotRepository(context.Background(), SelectedRepository{RepoPath: root})
	if err != nil {
		t.Fatal(err)
	}
	if skip := snapshot.DartRuntime[".fvmrc"].Extraction.Skip(); skip.Reason != runtimedeclaration.SkipReasonValueRejected || skip.Rejected != 1 {
		t.Fatalf("fvmrc skip = %+v, want value_rejected with 1 rejection", skip)
	}
	collected := buildStreamingGeneration(root, testCollectorRepositoryMetadata(root), "run-dart-guard", time.Now(), snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)
	rows := decodeDeclarationList(t, envelopes)
	if len(rows) != 1 || rows[0].RawSpec != "${DART_SDK}" || rows[0].SpecKind != "unresolved" {
		t.Fatalf("declarations = %+v", rows)
	}
	for _, envelope := range envelopes {
		body, err := json.Marshal(envelope.Payload)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "dart_runtime_evidence") || strings.Contains(string(body), "user:password@example.invalid") {
			t.Fatal("private Dart evidence or credential persisted")
		}
	}
}

func TestDartFvmrcStoresMetadataOnly(t *testing.T) {
	seed := "0123456789abcdef-private"
	snapshot, envelopes := snapshotWithPolicy(t, parser.DefaultRegistry(), ".fvmrc", "{\"flutter\":\"3.22.0\",\"token\":\""+seed+"\"}")
	if got := snapshot.ContentStores[".fvmrc"].Policy; got != parser.ContentStoreMetadata {
		t.Fatalf(".fvmrc policy = %s", got)
	}
	if got := contentFact(t, envelopes, ".fvmrc").Payload["content_body"]; got != "" {
		t.Fatalf("metadata body = %q", got)
	}
	assertNoSeedInAnyFact(t, envelopes, seed)
	rows := decodeDeclarationList(t, envelopes)
	if len(rows) != 1 || rows[0].RawSpec != "3.22.0" {
		t.Fatalf("runtime declarations = %+v", rows)
	}
}
