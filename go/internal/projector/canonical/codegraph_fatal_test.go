// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package canonical

// Regression tests for eshu-hq/eshu#7671: six repository scopes projected
// succeeded with file facts but zero File nodes.
//
// The fatal-decode half pins the swallowed-fatal bug: a file (or repository)
// fact whose schema major the projector does not support is a FATAL decode
// failure per decode.PartitionFailures ("so the caller propagates it"), not a
// per-fact quarantine. BuildMaterialization must fail loudly on it instead of
// returning a silent zero-File success.
//
// The empty-relative_path half pins the silent-skip bug: a file fact that
// decodes but carries an empty relative_path contributed no File row and no
// dead-letter. It must now quarantine as input_invalid so the drop is visible.

import (
	"errors"
	"testing"

	"github.com/eshu-hq/eshu-sdk/factschema"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func fatalDecodeFileFact(factID, schemaVersion string) facts.Envelope {
	return facts.Envelope{
		FactID:        factID,
		ScopeID:       "scope-1",
		GenerationID:  "gen-1",
		FactKind:      factschema.FactKindCodegraphFile,
		SchemaVersion: schemaVersion,
		Payload: map[string]any{
			"repo_id":          "repo-abc",
			"relative_path":    "cmd/main.go",
			"parsed_file_data": map[string]any{},
			"language":         "go",
		},
	}
}

func TestBuildMaterializationFailsLoudOnFatalFileDecode(t *testing.T) {
	t.Parallel()

	input := []facts.Envelope{
		{
			FactID:        "repository-1",
			ScopeID:       "scope-1",
			GenerationID:  "gen-1",
			FactKind:      factschema.FactKindCodegraphRepository,
			SchemaVersion: "1.0.0",
			Payload: map[string]any{
				"repo_id":    "repo-abc",
				"name":       "my-project",
				"local_path": "/repos/my-project",
			},
		},
		fatalDecodeFileFact("file-fatal", "2.0.0"),
	}

	_, _, err := BuildMaterialization(testScope(), testGeneration(), input)
	if err == nil {
		t.Fatal("BuildMaterialization() error = nil, want loud failure on unsupported schema major; a silent zero-File success is issue #7671")
	}
	if !errors.Is(err, factschema.ErrUnsupportedSchemaMajor) {
		t.Fatalf("BuildMaterialization() error = %v, want errors.Is ErrUnsupportedSchemaMajor", err)
	}
}

func TestBuildMaterializationFailsLoudOnFatalRepositoryDecode(t *testing.T) {
	t.Parallel()

	input := []facts.Envelope{
		{
			FactID:        "repository-fatal",
			ScopeID:       "scope-1",
			GenerationID:  "gen-1",
			FactKind:      factschema.FactKindCodegraphRepository,
			SchemaVersion: "2.0.0",
			Payload: map[string]any{
				"repo_id": "repo-abc",
				"name":    "my-project",
			},
		},
	}

	_, _, err := BuildMaterialization(testScope(), testGeneration(), input)
	if err == nil {
		t.Fatal("BuildMaterialization() error = nil, want loud failure on unsupported repository schema major")
	}
	if !errors.Is(err, factschema.ErrUnsupportedSchemaMajor) {
		t.Fatalf("BuildMaterialization() error = %v, want errors.Is ErrUnsupportedSchemaMajor", err)
	}
}

func TestBuildMaterializationQuarantinesEmptyRelativePath(t *testing.T) {
	t.Parallel()

	input := []facts.Envelope{
		{
			FactID:        "file-empty-path",
			ScopeID:       "scope-1",
			GenerationID:  "gen-1",
			FactKind:      factschema.FactKindCodegraphFile,
			SchemaVersion: "1.0.0",
			Payload: map[string]any{
				"repo_id":          "repo-abc",
				"relative_path":    "",
				"parsed_file_data": map[string]any{},
				"language":         "go",
			},
		},
	}

	result, quarantined, err := BuildMaterialization(testScope(), testGeneration(), input)
	if err != nil {
		t.Fatalf("BuildMaterialization() error = %v, want nil; an empty relative_path is a visible drop, not a fatal", err)
	}
	if len(result.Files) != 0 {
		t.Fatalf("len(Files) = %d, want 0; an empty relative_path must not materialize a row", len(result.Files))
	}
	if len(quarantined) != 1 {
		t.Fatalf("len(quarantined) = %d, want 1; the empty-relative_path file fact must dead-letter instead of skipping silently", len(quarantined))
	}
	if got := quarantined[0].FactID; got != "file-empty-path" {
		t.Fatalf("quarantined fact id = %q, want %q", got, "file-empty-path")
	}
	if got := quarantined[0].Field; got != "relative_path" {
		t.Fatalf("quarantined field = %q, want %q", got, "relative_path")
	}
}
