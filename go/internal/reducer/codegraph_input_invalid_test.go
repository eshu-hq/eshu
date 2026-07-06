// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestExtractCodeCallRowsQuarantinesFileMissingRepoID is the flagship
// regression test for Wave 4f S1 of Contract System v1 (issue #4749): the
// code-graph-core family's typed-decode migration. It proves the accuracy
// guarantee the migration exists to protect: a "file" fact missing its
// required repo_id key dead-letters as a per-fact input_invalid quarantine
// via partitionDecodeFailures, NOT an empty-string graph identity.
//
// Before the migration this behavior was impossible: extractCodeCallRowsWithIndex
// read repo_id with payloadStr, which returns "" for the absent key, and the
// malformed fact was silently skipped by the `if repositoryID == ""` guard
// with no operator-visible signal — no quarantinedFact was ever recorded.
// After the migration, extractCodeCallRowsWithIndex decodes each "file"
// fact's outer envelope through factschema.DecodeCodegraphFile
// (decodeCodegraphFile); the malformed fact yields a classified
// *factDecodeError that partitionDecodeFailures routes to an explicit
// quarantinedFact naming the missing field, while a valid sibling "file"
// fact in the same batch still produces its code-call edges (per-fact
// isolation, the same contract every prior Contract System v1 wave
// established).
func TestExtractCodeCallRowsQuarantinesFileMissingRepoID(t *testing.T) {
	t.Parallel()

	// A "file" fact whose required repo_id key is ABSENT (not merely empty):
	// the exact malformed input the AC names. Everything else is present so
	// the ONLY reason to quarantine the fact is the missing required field.
	malformed := facts.Envelope{
		FactID:   "malformed-file-missing-repo-id",
		FactKind: "file",
		Payload: map[string]any{
			// "repo_id" intentionally absent.
			"graph_id":      "orphan:app.py",
			"graph_kind":    "file",
			"relative_path": "app.py",
			"parsed_file_data": map[string]any{
				"path": "/repo/app.py",
				"functions": []any{
					map[string]any{"name": "orphanFn", "line_number": 1, "end_line": 3, "uid": "uid:orphan-fn"},
				},
			},
			"is_dependency": false,
		},
	}

	// A fully valid, independent file fact carrying a real call edge that
	// must still project despite the malformed fact sharing the batch. This
	// is the isolation half of the contract.
	valid := facts.Envelope{
		FactID:   "valid-file",
		FactKind: "file",
		Payload: map[string]any{
			"graph_id":      "repo-quarantine:main.py",
			"graph_kind":    "file",
			"repo_id":       "repo-quarantine",
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
			"is_dependency": false,
		},
	}

	envelopes := []facts.Envelope{malformed, valid}
	validEnvelopes, quarantined := partitionCodegraphFileFacts(envelopes)
	repositoryIDs := collectCodeCallRepositoryIDs(validEnvelopes)
	entityIndex := buildCodeEntityIndex(validEnvelopes)
	repositoryImports := collectCodeCallRepositoryImports(validEnvelopes)
	reexportIndex := buildCodeCallReexportIndex(validEnvelopes)

	_, rows := extractCodeCallRowsWithIndex(validEnvelopes, repositoryIDs, entityIndex, repositoryImports, reexportIndex)

	// The malformed fact must be recorded as EXACTLY one input_invalid
	// quarantine naming the missing field and the fact id — the visible
	// dead-letter this migration exists to produce.
	if len(quarantined) != 1 {
		t.Fatalf("len(quarantined) = %d, want 1; the missing-repo_id file fact must be quarantined via partitionDecodeFailures", len(quarantined))
	}
	if quarantined[0].field != "repo_id" {
		t.Fatalf("quarantined[0].field = %q, want %q", quarantined[0].field, "repo_id")
	}
	if quarantined[0].classification != "input_invalid" {
		t.Fatalf("quarantined[0].classification = %q, want %q", quarantined[0].classification, "input_invalid")
	}
	if quarantined[0].factID != "malformed-file-missing-repo-id" {
		t.Fatalf("quarantined[0].factID = %q, want %q", quarantined[0].factID, "malformed-file-missing-repo-id")
	}

	// Per-fact isolation: the valid sibling file still produces its
	// caller->callee edge, and no row anywhere carries an empty-string
	// repo_id (the pre-migration failure mode this test guards against).
	foundValidEdge := false
	for _, row := range rows {
		if row["repo_id"] == "" {
			t.Fatalf("extractCodeCallRowsWithIndex produced a row with an empty-string repo_id: %#v; the malformed fact must dead-letter, never emit an edge under an empty identity", row)
		}
		if row["repo_id"] == "repo-quarantine" &&
			row["caller_entity_id"] == "uid:caller" &&
			row["callee_entity_id"] == "uid:callee" {
			foundValidEdge = true
		}
	}
	if !foundValidEdge {
		t.Fatalf("extractCodeCallRowsWithIndex rows = %#v, want the valid sibling file's caller->callee edge to still project despite the quarantined malformed file in the same batch", rows)
	}
}

// TestExtractCodeCallRowsQuarantinesFileMissingRelativePath mirrors the
// repo_id case above for the "file" fact's other required join-identity
// field: relative_path. A file missing relative_path cannot form the
// repo_id+relative_path graph identity code-call extraction and the
// code-import repo-edge builders both rely on, so it must dead-letter rather
// than silently combine with an empty-string path segment.
func TestExtractCodeCallRowsQuarantinesFileMissingRelativePath(t *testing.T) {
	t.Parallel()

	malformed := facts.Envelope{
		FactID:   "malformed-file-missing-relative-path",
		FactKind: "file",
		Payload: map[string]any{
			"graph_id":   "repo-quarantine-2:",
			"graph_kind": "file",
			"repo_id":    "repo-quarantine-2",
			// "relative_path" intentionally absent.
			"parsed_file_data": map[string]any{
				"path": "/repo/orphan.py",
			},
			"is_dependency": false,
		},
	}

	valid := facts.Envelope{
		FactID:   "valid-file-2",
		FactKind: "file",
		Payload: map[string]any{
			"graph_id":      "repo-quarantine-2:main.py",
			"graph_kind":    "file",
			"repo_id":       "repo-quarantine-2",
			"relative_path": "main.py",
			"parsed_file_data": map[string]any{
				"path": "/repo/main.py",
				"functions": []any{
					map[string]any{"name": "caller2", "line_number": 1, "end_line": 5, "uid": "uid:caller2"},
					map[string]any{"name": "callee2", "line_number": 7, "end_line": 9, "uid": "uid:callee2"},
				},
				"function_calls": []any{
					map[string]any{"name": "callee2", "full_name": "callee2", "line_number": 2, "lang": "python"},
				},
			},
			"is_dependency": false,
		},
	}

	envelopes := []facts.Envelope{malformed, valid}
	validEnvelopes, quarantined := partitionCodegraphFileFacts(envelopes)
	repositoryIDs := collectCodeCallRepositoryIDs(validEnvelopes)
	entityIndex := buildCodeEntityIndex(validEnvelopes)
	repositoryImports := collectCodeCallRepositoryImports(validEnvelopes)
	reexportIndex := buildCodeCallReexportIndex(validEnvelopes)

	_, rows := extractCodeCallRowsWithIndex(validEnvelopes, repositoryIDs, entityIndex, repositoryImports, reexportIndex)

	if len(quarantined) != 1 {
		t.Fatalf("len(quarantined) = %d, want 1; the missing-relative_path file fact must be quarantined via partitionDecodeFailures", len(quarantined))
	}
	if quarantined[0].field != "relative_path" {
		t.Fatalf("quarantined[0].field = %q, want %q", quarantined[0].field, "relative_path")
	}
	if quarantined[0].classification != "input_invalid" {
		t.Fatalf("quarantined[0].classification = %q, want %q", quarantined[0].classification, "input_invalid")
	}

	foundValidEdge := false
	for _, row := range rows {
		if row["caller_entity_id"] == "uid:caller2" && row["callee_entity_id"] == "uid:callee2" {
			foundValidEdge = true
		}
	}
	if !foundValidEdge {
		t.Fatalf("extractCodeCallRowsWithIndex rows = %#v, want the valid sibling file's caller->callee edge to still project despite the file missing relative_path being quarantined", rows)
	}
}

// TestDecodeCodegraphTreatsAbsentSchemaVersionAsLatestMajor pins the guarantee
// that keeps the code family decoding correctly WITHOUT being registered as a
// schema-version-admitted fact kind (registry + admission are deferred to issue
// #4752): the git collector emits "file"/"repository" facts with NO
// SchemaVersion on the envelope, so decodeCodegraphFile/decodeCodegraphRepository
// (via factschemaEnvelope's empty->"1.0.0" normalization + decodeLatestMajor)
// MUST decode an absent-version fact as the latest major, and MUST still
// dead-letter a fact missing a required identity field as input_invalid. If a
// later change ever makes an absent SchemaVersion fail decode, this test fails
// loudly rather than silently dropping every real code-graph fact.
func TestDecodeCodegraphTreatsAbsentSchemaVersionAsLatestMajor(t *testing.T) {
	t.Parallel()

	// A valid "file" fact with NO SchemaVersion (exactly what the collector
	// emits today) must decode cleanly to its typed identity.
	validFile := facts.Envelope{
		FactID:   "no-version-file",
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":          "repo-noversion",
			"relative_path":    "app.go",
			"parsed_file_data": map[string]any{"path": "/repo/app.go"},
		},
	}
	file, err := decodeCodegraphFile(validFile)
	if err != nil {
		t.Fatalf("decodeCodegraphFile(no SchemaVersion) error = %v, want nil (an absent version must decode as the latest major)", err)
	}
	if file.RepoID != "repo-noversion" || file.RelativePath != "app.go" {
		t.Fatalf("decodeCodegraphFile identity = {RepoID:%q RelativePath:%q}, want {repo-noversion app.go}", file.RepoID, file.RelativePath)
	}

	// A valid "repository" fact with NO SchemaVersion must likewise decode.
	validRepo := facts.Envelope{
		FactID:   "no-version-repo",
		FactKind: "repository",
		Payload:  map[string]any{"repo_id": "repo-noversion"},
	}
	repo, err := decodeCodegraphRepository(validRepo)
	if err != nil {
		t.Fatalf("decodeCodegraphRepository(no SchemaVersion) error = %v, want nil", err)
	}
	if repo.RepoID != "repo-noversion" {
		t.Fatalf("decodeCodegraphRepository RepoID = %q, want repo-noversion", repo.RepoID)
	}

	// A "file" fact missing its required repo_id (still no SchemaVersion) must
	// dead-letter as input_invalid, not decode to an empty identity.
	missingRepoID := facts.Envelope{
		FactID:   "no-version-missing-repo-id",
		FactKind: "file",
		Payload: map[string]any{
			"relative_path":    "app.go",
			"parsed_file_data": map[string]any{"path": "/repo/app.go"},
		},
	}
	if _, err := decodeCodegraphFile(missingRepoID); err == nil {
		t.Fatalf("decodeCodegraphFile(missing repo_id) error = nil, want an input_invalid *factDecodeError")
	} else {
		q, quarantinable, fatal := partitionDecodeFailures(missingRepoID, err)
		if !quarantinable {
			t.Fatalf("partitionDecodeFailures classified the missing-repo_id decode error as fatal (%v), want a quarantinable input_invalid", fatal)
		}
		if q.field != "repo_id" || q.classification != "input_invalid" {
			t.Fatalf("quarantinedFact = {field:%q classification:%q}, want {repo_id input_invalid}", q.field, q.classification)
		}
	}
}

// TestExtractCodeCallRowsQuarantinesFileNonObjectParsedFileData proves the
// "parsed_file_data must be a JSON object" half of the contract: a "file" fact
// whose required parsed_file_data key is present but is NOT an object (here a
// string) fails decode and is recorded as a visible quarantine, never a silent
// drop and never a proceed-with-nil-AST. This exercises the residual
// decode-failure path (a type mismatch, which partitionDecodeFailures returns
// as a quarantinable input_invalid) end to end through
// extractCodeCallRowsWithIndex.
func TestExtractCodeCallRowsQuarantinesFileNonObjectParsedFileData(t *testing.T) {
	t.Parallel()

	malformed := facts.Envelope{
		FactID:   "malformed-file-non-object-parsed-file-data",
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":       "repo-nonobject",
			"relative_path": "app.go",
			// parsed_file_data is present but a string, not an object — the
			// decoder cannot assign it to map[string]any and must reject it.
			"parsed_file_data": "not-an-object",
		},
	}
	valid := facts.Envelope{
		FactID:   "valid-file-nonobject-sibling",
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":       "repo-nonobject",
			"relative_path": "main.go",
			"parsed_file_data": map[string]any{
				"path": "/repo/main.go",
				"functions": []any{
					map[string]any{"name": "callerN", "line_number": 1, "end_line": 5, "uid": "uid:callerN"},
					map[string]any{"name": "calleeN", "line_number": 7, "end_line": 9, "uid": "uid:calleeN"},
				},
				"function_calls": []any{
					map[string]any{"name": "calleeN", "full_name": "calleeN", "line_number": 2, "lang": "go"},
				},
			},
		},
	}

	envelopes := []facts.Envelope{malformed, valid}
	validEnvelopes, quarantined := partitionCodegraphFileFacts(envelopes)
	repositoryIDs := collectCodeCallRepositoryIDs(validEnvelopes)
	entityIndex := buildCodeEntityIndex(validEnvelopes)
	repositoryImports := collectCodeCallRepositoryImports(validEnvelopes)
	reexportIndex := buildCodeCallReexportIndex(validEnvelopes)

	_, rows := extractCodeCallRowsWithIndex(validEnvelopes, repositoryIDs, entityIndex, repositoryImports, reexportIndex)

	if len(quarantined) != 1 {
		t.Fatalf("len(quarantined) = %d, want 1; a non-object parsed_file_data must be quarantined, never silently dropped", len(quarantined))
	}
	if quarantined[0].factID != "malformed-file-non-object-parsed-file-data" {
		t.Fatalf("quarantined[0].factID = %q, want the non-object fact", quarantined[0].factID)
	}
	if quarantined[0].classification != "input_invalid" {
		t.Fatalf("quarantined[0].classification = %q, want input_invalid", quarantined[0].classification)
	}

	foundValidEdge := false
	for _, row := range rows {
		if row["caller_entity_id"] == "uid:callerN" && row["callee_entity_id"] == "uid:calleeN" {
			foundValidEdge = true
		}
	}
	if !foundValidEdge {
		t.Fatalf("extractCodeCallRowsWithIndex rows = %#v, want the valid sibling's edge to still project despite the non-object parsed_file_data quarantine", rows)
	}
}
