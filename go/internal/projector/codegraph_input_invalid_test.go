// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"errors"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

func TestBuildCanonicalMaterializationQuarantinesMissingCodegraphRepositoryID(t *testing.T) {
	t.Parallel()

	validRepository := facts.Envelope{
		FactID:        "repository-good",
		ScopeID:       "scope-1",
		GenerationID:  "gen-1",
		FactKind:      factschema.FactKindCodegraphRepository,
		SchemaVersion: "1.0.0",
		Payload: map[string]any{
			"repo_id":    "repo-abc",
			"name":       "my-project",
			"local_path": "/repos/my-project",
		},
	}
	malformed := facts.Envelope{
		FactID:        "repository-bad",
		ScopeID:       "scope-1",
		GenerationID:  "gen-1",
		FactKind:      factschema.FactKindCodegraphRepository,
		SchemaVersion: "1.0.0",
		Payload: map[string]any{
			// "repo_id" intentionally absent.
			"name": "unattributed",
		},
	}

	result, quarantined := buildCanonicalMaterialization(
		testScope(),
		testGeneration(),
		[]facts.Envelope{malformed, validRepository},
	)

	if len(quarantined) != 1 {
		t.Fatalf("len(quarantined) = %d, want 1; the missing-repo_id repository fact must be quarantined", len(quarantined))
	}
	if got := quarantined[0].factKind; got != factschema.FactKindCodegraphRepository {
		t.Fatalf("quarantined fact kind = %q, want %q", got, factschema.FactKindCodegraphRepository)
	}
	if got := quarantined[0].field; got != "repo_id" {
		t.Fatalf("quarantined field = %q, want %q", got, "repo_id")
	}
	if got := quarantined[0].factID; got != "repository-bad" {
		t.Fatalf("quarantined fact id = %q, want %q", got, "repository-bad")
	}

	if result.Repository == nil {
		t.Fatal("Repository is nil; the valid sibling repository fact must still project")
	}
	if got := result.Repository.RepoID; got != "repo-abc" {
		t.Fatalf("Repository.RepoID = %q, want %q", got, "repo-abc")
	}
}

func TestBuildCanonicalMaterializationQuarantinesMissingCodegraphFilePath(t *testing.T) {
	t.Parallel()

	validFile := facts.Envelope{
		FactID:        "file-good",
		ScopeID:       "scope-1",
		GenerationID:  "gen-1",
		FactKind:      factschema.FactKindCodegraphFile,
		SchemaVersion: "1.0.0",
		Payload: map[string]any{
			"repo_id":          "repo-abc",
			"relative_path":    "cmd/main.go",
			"parsed_file_data": map[string]any{},
			"language":         "go",
		},
	}
	malformed := facts.Envelope{
		FactID:        "file-bad",
		ScopeID:       "scope-1",
		GenerationID:  "gen-1",
		FactKind:      factschema.FactKindCodegraphFile,
		SchemaVersion: "1.0.0",
		Payload: map[string]any{
			"repo_id":          "repo-abc",
			"parsed_file_data": map[string]any{},
			"language":         "go",
		},
	}

	result, quarantined := buildCanonicalMaterialization(
		testScope(),
		testGeneration(),
		[]facts.Envelope{validFile, malformed},
	)

	if len(quarantined) != 1 {
		t.Fatalf("len(quarantined) = %d, want 1; the missing-relative_path file fact must be quarantined", len(quarantined))
	}
	if got := quarantined[0].factKind; got != factschema.FactKindCodegraphFile {
		t.Fatalf("quarantined fact kind = %q, want %q", got, factschema.FactKindCodegraphFile)
	}
	if got := quarantined[0].field; got != "relative_path" {
		t.Fatalf("quarantined field = %q, want %q", got, "relative_path")
	}
	if got := quarantined[0].factID; got != "file-bad" {
		t.Fatalf("quarantined fact id = %q, want %q", got, "file-bad")
	}

	if len(result.Files) != 1 {
		t.Fatalf("len(Files) = %d, want 1; the valid file must still project despite the quarantined sibling", len(result.Files))
	}
	if got := result.Files[0].Path; got != "/repos/my-project/cmd/main.go" {
		t.Fatalf("valid file path = %q, want %q", got, "/repos/my-project/cmd/main.go")
	}
}

func TestBuildCanonicalMaterializationPresentButEmptyCodegraphFilePathIsDroppedNotQuarantined(t *testing.T) {
	t.Parallel()

	emptyPathFile := facts.Envelope{
		FactID:        "file-empty",
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
	}

	result, quarantined := buildCanonicalMaterialization(
		testScope(),
		testGeneration(),
		[]facts.Envelope{emptyPathFile},
	)

	if len(quarantined) != 0 {
		t.Fatalf("len(quarantined) = %d, want 0; a present-but-empty relative_path is a valid decode, not a quarantine", len(quarantined))
	}
	if len(result.Files) != 0 {
		t.Fatalf("len(Files) = %d, want 0; a file with an empty relative_path is incomplete and must be dropped", len(result.Files))
	}
}

func TestBuildCanonicalMaterializationWhitespaceOnlyCodegraphFilePathIsDroppedNotQuarantined(t *testing.T) {
	t.Parallel()

	whitespacePathFile := facts.Envelope{
		FactID:        "file-whitespace",
		ScopeID:       "scope-1",
		GenerationID:  "gen-1",
		FactKind:      factschema.FactKindCodegraphFile,
		SchemaVersion: "1.0.0",
		Payload: map[string]any{
			"repo_id":          "repo-abc",
			"relative_path":    " \t ",
			"parsed_file_data": map[string]any{},
			"language":         "go",
		},
	}

	result, quarantined := buildCanonicalMaterialization(
		testScope(),
		testGeneration(),
		[]facts.Envelope{whitespacePathFile},
	)

	if len(quarantined) != 0 {
		t.Fatalf("len(quarantined) = %d, want 0; a whitespace-only relative_path is a valid decode, not a quarantine", len(quarantined))
	}
	if len(result.Files) != 0 {
		t.Fatalf("len(Files) = %d, want 0; a file with a whitespace-only relative_path is incomplete and must be dropped", len(result.Files))
	}
}

func TestBuildProjectionRejectsUnsupportedCodegraphSchemaMajor(t *testing.T) {
	t.Parallel()

	scopeValue := testScope()
	generation := testGeneration()
	_, err := buildProjection(scopeValue, generation, []facts.Envelope{
		{
			FactID:        "repository-v2",
			ScopeID:       scopeValue.ScopeID,
			GenerationID:  generation.GenerationID,
			FactKind:      factschema.FactKindCodegraphRepository,
			SchemaVersion: "2.0.0",
			Payload: map[string]any{
				"repo_id":    "repo-abc",
				"name":       "my-project",
				"local_path": "/repos/my-project",
			},
		},
	})

	if err == nil {
		t.Fatal("buildProjection() error = nil, want unsupported codegraph schema major")
	}
	if !errors.Is(err, factschema.ErrUnsupportedSchemaMajor) {
		t.Fatalf("buildProjection() error = %v, want errors.Is ErrUnsupportedSchemaMajor", err)
	}
}

// TestBuildProjectionAcceptsPersistedVersionlessCodegraphRepository is the #4893
// regression guard for the #4899 admission gate: the git collector emits
// repository facts with no SchemaVersion and the Postgres persist layer stamps
// them "0.0.0", so a fact LOADED for projection carries "0.0.0". The admission
// gate (validateCodegraphFactSchemaVersion, which runs in buildProjection BEFORE
// the typed decode adapter) must accept that sentinel; #4899 rejected it, so
// buildProjection failed before canonical materialization and no generation ever
// activated. This exercises buildProjection, not only buildCanonicalMaterialization.
func TestBuildProjectionAcceptsPersistedVersionlessCodegraphRepository(t *testing.T) {
	t.Parallel()

	scopeValue := testScope()
	generation := testGeneration()
	_, err := buildProjection(scopeValue, generation, []facts.Envelope{
		{
			FactID:        "repository-versionless",
			ScopeID:       scopeValue.ScopeID,
			GenerationID:  generation.GenerationID,
			FactKind:      factschema.FactKindCodegraphRepository,
			SchemaVersion: "0.0.0", // persist-layer sentinel for a version-less collector fact
			Payload: map[string]any{
				"repo_id":    "repo-abc",
				"name":       "my-project",
				"local_path": "/repos/my-project",
			},
		},
	})
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil; a persisted \"0.0.0\" repository fact must be admitted", err)
	}
}

// TestBuildProjectionAcceptsPersistedVersionlessCodegraphFile is the companion
// #4893 regression guard for a persisted version-less codegraph file fact.
func TestBuildProjectionAcceptsPersistedVersionlessCodegraphFile(t *testing.T) {
	t.Parallel()

	scopeValue := testScope()
	generation := testGeneration()
	_, err := buildProjection(scopeValue, generation, []facts.Envelope{
		{
			FactID:        "file-versionless",
			ScopeID:       scopeValue.ScopeID,
			GenerationID:  generation.GenerationID,
			FactKind:      factschema.FactKindCodegraphFile,
			SchemaVersion: "0.0.0",
			Payload: map[string]any{
				"repo_id":          "repo-abc",
				"relative_path":    "cmd/main.go",
				"parsed_file_data": map[string]any{},
				"language":         "go",
			},
		},
	})
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil; a persisted \"0.0.0\" file fact must be admitted", err)
	}
}

func BenchmarkBuildCanonicalMaterializationCodegraphFiles(b *testing.B) {
	sc := testScope()
	gen := testGeneration()
	envelopes := benchmarkCodegraphFacts(1000)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, quarantined := buildCanonicalMaterialization(sc, gen, envelopes)
		if len(quarantined) != 0 {
			b.Fatalf("len(quarantined) = %d, want 0", len(quarantined))
		}
		if got := len(result.Files); got != 1000 {
			b.Fatalf("len(Files) = %d, want 1000", got)
		}
	}
}

func benchmarkCodegraphFacts(fileCount int) []facts.Envelope {
	envelopes := make([]facts.Envelope, 0, fileCount+1)
	envelopes = append(envelopes, facts.Envelope{
		FactID:        "repository-bench",
		ScopeID:       "scope-1",
		GenerationID:  "gen-1",
		FactKind:      factschema.FactKindCodegraphRepository,
		SchemaVersion: "1.0.0",
		Payload: map[string]any{
			"repo_id":    "repo-abc",
			"name":       "my-project",
			"local_path": "/repos/my-project",
			"remote_url": "https://github.com/example/my-project.git",
		},
	})
	for i := 0; i < fileCount; i++ {
		relativePath := fmt.Sprintf("src/pkg%03d/file%03d.go", i%50, i)
		envelopes = append(envelopes, facts.Envelope{
			FactID:        fmt.Sprintf("file-bench-%04d", i),
			ScopeID:       "scope-1",
			GenerationID:  "gen-1",
			FactKind:      factschema.FactKindCodegraphFile,
			SchemaVersion: "1.0.0",
			Payload: map[string]any{
				"repo_id":          "repo-abc",
				"relative_path":    relativePath,
				"parsed_file_data": map[string]any{},
				"language":         "go",
			},
		})
	}
	return envelopes
}

// TestBuildCanonicalMaterializationProjectsPersistedVersionlessCodegraphFile is
// the #4893 regression guard for the #4899 codegraph typed-decode: the git
// collector emits file/repository facts with no SchemaVersion, and the Postgres
// persist layer stamps a version-less fact as "0.0.0", so a fact LOADED for
// projection carries "0.0.0". Before the fix the projector's factschemaEnvelope
// normalized only the "" spelling, so a real "0.0.0" file fact failed the
// decode's supported-major check and was quarantined/dropped, projecting zero
// File rows (the proof-domain content-write regression). After the fix the
// "0.0.0" sentinel is normalized to the latest major and the file projects.
func TestBuildCanonicalMaterializationProjectsPersistedVersionlessCodegraphFile(t *testing.T) {
	t.Parallel()

	versionlessFile := facts.Envelope{
		FactID:        "file-versionless",
		ScopeID:       "scope-1",
		GenerationID:  "gen-1",
		FactKind:      factschema.FactKindCodegraphFile,
		SchemaVersion: "0.0.0", // persist-layer sentinel for a collector-emitted version-less fact
		Payload: map[string]any{
			"repo_id":          "repo-abc",
			"relative_path":    "cmd/main.go",
			"parsed_file_data": map[string]any{},
			"language":         "go",
		},
	}

	result, quarantined := buildCanonicalMaterialization(
		testScope(),
		testGeneration(),
		[]facts.Envelope{versionlessFile},
	)

	if len(quarantined) != 0 {
		t.Fatalf("len(quarantined) = %d, want 0; a persisted \"0.0.0\" file fact must decode, not dead-letter", len(quarantined))
	}
	if len(result.Files) != 1 {
		t.Fatalf("len(Files) = %d, want 1; the persisted version-less file fact must project", len(result.Files))
	}
	if got := result.Files[0].Path; got != "/repos/my-project/cmd/main.go" {
		t.Fatalf("file path = %q, want %q", got, "/repos/my-project/cmd/main.go")
	}
}
