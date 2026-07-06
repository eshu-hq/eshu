// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestDecodeCodegraphAcceptsPersistedVersionlessSchemaVersion is the corpus-gate
// regression test (PR #4753 / issue #4749). It reproduces at unit scale the P0
// the full golden-corpus gate caught: EVERY real "file"/"repository" fact was
// dead-lettering, collapsing the whole code graph (rc-2/8/10/11/12/13/15/23 all
// to zero).
//
// Root cause: the git collector emits these facts with NO SchemaVersion
// (git_followup_facts.go factEnvelope). The Postgres persist layer stores a
// version-less fact as the sentinel "0.0.0"
// (go/internal/storage/postgres/facts.go, facts_streaming.go
// emptyToDefault(SchemaVersion, "0.0.0")), so a fact LOADED at reducer time
// carries SchemaVersion="0.0.0", not "". decodeLatestMajor only accepts
// major=="1" and major("0.0.0")=="0", so the fact hit the default branch and
// dead-lettered as ErrUnsupportedSchemaMajor. factschemaEnvelope only
// normalized an EMPTY version, not the persisted "0.0.0" sentinel — the exact
// SchemaVersion a real loaded fact carries — which is why the absent-version
// unit test passed while the corpus broke.
//
// The fix normalizes the persisted version-less sentinel ("0.0.0") the same as
// empty in factschemaEnvelope, so a real loaded code-graph fact decodes as the
// latest major. This test uses SchemaVersion="0.0.0" (what real facts carry),
// NOT absent, so it exercises the production path the earlier test missed.
func TestDecodeCodegraphAcceptsPersistedVersionlessSchemaVersion(t *testing.T) {
	t.Parallel()

	// A valid "file" fact carrying the persisted version-less sentinel must
	// decode to its typed identity, not dead-letter.
	persistedFile := facts.Envelope{
		FactID:        "persisted-version-file",
		FactKind:      "file",
		SchemaVersion: "0.0.0", // what the Postgres load path returns for a version-less fact
		Payload: map[string]any{
			"repo_id":          "repo-persisted",
			"relative_path":    "app.go",
			"parsed_file_data": map[string]any{"path": "/repo/app.go"},
		},
	}
	file, err := decodeCodegraphFile(persistedFile)
	if err != nil {
		t.Fatalf("decodeCodegraphFile(SchemaVersion=0.0.0) error = %v, want nil; the persisted version-less sentinel a real loaded fact carries must decode as the latest major, not dead-letter (this is the corpus-gate P0)", err)
	}
	if file.RepoID != "repo-persisted" || file.RelativePath != "app.go" {
		t.Fatalf("decodeCodegraphFile identity = {RepoID:%q RelativePath:%q}, want {repo-persisted app.go}", file.RepoID, file.RelativePath)
	}

	persistedRepo := facts.Envelope{
		FactID:        "persisted-version-repo",
		FactKind:      "repository",
		SchemaVersion: "0.0.0",
		Payload:       map[string]any{"repo_id": "repo-persisted"},
	}
	if repo, err := decodeCodegraphRepository(persistedRepo); err != nil {
		t.Fatalf("decodeCodegraphRepository(SchemaVersion=0.0.0) error = %v, want nil", err)
	} else if repo.RepoID != "repo-persisted" {
		t.Fatalf("decodeCodegraphRepository RepoID = %q, want repo-persisted", repo.RepoID)
	}
}

// TestExtractCodeCallRowsProducesRowsForPersistedVersionlessFacts reproduces the
// corpus rc-11 (Function-CALLS-Function count=0) failure at unit scale: real
// "file" facts carry SchemaVersion="0.0.0", and before the fix every one
// quarantined at extractCodeCallRowsWithIndex, so ZERO code-call rows were
// produced across the whole corpus. This asserts a valid persisted-version file
// still produces its caller->callee edge and is NOT quarantined.
func TestExtractCodeCallRowsProducesRowsForPersistedVersionlessFacts(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactID:        "persisted-repo",
			FactKind:      "repository",
			SchemaVersion: "0.0.0",
			Payload:       map[string]any{"repo_id": "repo-cg", "source_run_id": "run-1"},
		},
		{
			FactID:        "persisted-file",
			FactKind:      "file",
			SchemaVersion: "0.0.0", // the persisted version-less sentinel real facts carry
			Payload: map[string]any{
				"repo_id":       "repo-cg",
				"relative_path": "main.py",
				"parsed_file_data": map[string]any{
					"path": "/repo/main.py",
					"functions": []any{
						map[string]any{"name": "caller", "line_number": 1, "end_line": 5, "uid": "uid:caller"},
						map[string]any{"name": "callee", "line_number": 7, "end_line": 9, "uid": "uid:callee"},
					},
					"function_calls": []any{
						map[string]any{"name": "callee", "full_name": "callee", "line_number": 2, "lang": "python"},
					},
				},
			},
		},
	}

	validEnvelopes, quarantined := partitionCodegraphFileFacts(envelopes)
	repositoryIDs := collectCodeCallRepositoryIDs(validEnvelopes)
	entityIndex := buildCodeEntityIndex(validEnvelopes)
	repositoryImports := collectCodeCallRepositoryImports(validEnvelopes)
	reexportIndex := buildCodeCallReexportIndex(validEnvelopes)

	_, rows := extractCodeCallRowsWithIndex(validEnvelopes, repositoryIDs, entityIndex, repositoryImports, reexportIndex)

	if len(quarantined) != 0 {
		t.Fatalf("len(quarantined) = %d, want 0; a valid persisted-version file fact must NOT be quarantined (the corpus P0 was every valid file quarantining): %+v", len(quarantined), quarantined)
	}
	foundEdge := false
	for _, row := range rows {
		if row["caller_entity_id"] == "uid:caller" && row["callee_entity_id"] == "uid:callee" {
			foundEdge = true
		}
	}
	if !foundEdge {
		t.Fatalf("extractCodeCallRowsWithIndex produced no caller->callee row for a valid persisted-version file (rc-11 CALLS count=0 at unit scale); rows=%#v", rows)
	}
}
