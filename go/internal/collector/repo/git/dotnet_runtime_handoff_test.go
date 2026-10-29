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

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestDotNetRuntimeNativeHandoff(t *testing.T) {
	root := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(root, "global.json"), "{\n  \"sdk\": {\"version\": \"8.0.300\"}\n}\n")
	writeCollectorTestFile(t, filepath.Join(root, "App.csproj"), "<Project>\n  <PropertyGroup>\n    <TargetFramework>net8.0</TargetFramework>\n    <TargetFrameworks>net8.0;net9.0</TargetFrameworks>\n  </PropertyGroup>\n</Project>\n")
	observed := time.Date(2026, time.October, 29, 0, 0, 0, 0, time.UTC)
	snapshot, err := (NativeRepositorySnapshotter{Now: func() time.Time { return observed }}).SnapshotRepository(context.Background(), SelectedRepository{RepoPath: root})
	if err != nil {
		t.Fatal(err)
	}
	collected := buildStreamingGeneration(root, testCollectorRepositoryMetadata(root), "run-dotnet", observed, snapshot, false, "")
	got := decodeDeclarationList(t, drainFactChannel(collected.Facts))
	if len(got) != 4 {
		t.Fatalf(".NET declarations = %+v, want four", got)
	}
	want := map[string]bool{
		"dotnet_global_json_sdk:8.0.300":         true,
		"dotnet_csproj_target_framework:net8.0":  true,
		"dotnet_csproj_target_frameworks:net8.0": true,
		"dotnet_csproj_target_frameworks:net9.0": true,
	}
	for _, row := range got {
		key := row.SourceKind + ":" + row.RawSpec
		if !want[key] || row.Runtime != "dotnet" {
			t.Errorf("declaration = %+v", row)
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Fatalf("missing .NET declarations: %+v", want)
	}
}

func TestDotNetRuntimeRejectsCredentialAndConditionalEvidence(t *testing.T) {
	root := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(root, "global.json"), `{"sdk":{"version":"https://user:password@example.invalid/sdk"}}`)
	writeCollectorTestFile(t, filepath.Join(root, "Unsafe.csproj"), `<Project>
  <PropertyGroup Condition="'$(Configuration)' == 'Debug'">
    <TargetFramework>net8.0</TargetFramework>
  </PropertyGroup>
  <PropertyGroup>
    <TargetFramework>${TFM}</TargetFramework>
  </PropertyGroup>
</Project>`)
	snapshot, err := (NativeRepositorySnapshotter{}).SnapshotRepository(context.Background(), SelectedRepository{RepoPath: root})
	if err != nil {
		t.Fatal(err)
	}
	collected := buildStreamingGeneration(root, testCollectorRepositoryMetadata(root), "run-dotnet-guard", time.Now(), snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)
	rows := decodeDeclarationList(t, envelopes)
	if len(rows) != 1 || rows[0].RawSpec != "${TFM}" || rows[0].SpecKind != "unresolved" {
		t.Fatalf("declarations = %+v", rows)
	}
	for _, envelope := range envelopes {
		body, err := json.Marshal(envelope.Payload)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "dotnet_runtime_evidence") || strings.Contains(string(body), "user:password@example.invalid") {
			t.Fatal("private .NET evidence or credential persisted")
		}
	}
}

func TestDotNetGlobalJSONStoresMetadataOnly(t *testing.T) {
	seed := "0123456789abcdef-private"
	snapshot, envelopes := snapshotWithPolicy(t, parser.DefaultRegistry(), "global.json", `{"sdk":{"version":"8.0.300"},"token":"`+seed+`"}`)
	if got := snapshot.ContentStores["global.json"].Policy; got != parser.ContentStoreMetadata {
		t.Fatalf("global.json policy = %s", got)
	}
	if got := contentFact(t, envelopes, "global.json").Payload["content_body"]; got != "" {
		t.Fatalf("metadata body = %q", got)
	}
	assertNoSeedInAnyFact(t, envelopes, seed)
	rows := decodeDeclarationList(t, envelopes)
	if len(rows) != 1 || rows[0].RawSpec != "8.0.300" {
		t.Fatalf("runtime declarations = %+v", rows)
	}
}
