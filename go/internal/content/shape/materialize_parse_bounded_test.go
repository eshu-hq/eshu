// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shape

import "testing"

// TestMaterializeParseBoundedFilePurgesStaleEntities verifies that a file
// whose tree-sitter parse was skipped by the #4766 byte cap (ParseBounded)
// sets PurgeEntities on its content record, mirroring the existing per-file
// entity-count cap precedent (materializeFile / fileEntityCapHit). Without
// this, a JS/TS/TSX/PHP file that was previously indexed with real entities
// and later grows past the parser's byte cap keeps its stale
// content_entities rows queryable forever, because the writer only retracts
// entities when Record.PurgeEntities is set.
//
// Regression: #4766 added the parse byte cap to javascript.Parse/php.Parse,
// returning an empty entity payload plus a js_parse_bounded/php_parse_bounded
// marker, but materializeFile never consulted that marker -- only the
// unrelated entity-count cap set PurgeEntities. A path re-materialized as
// bounded therefore produced zero new entities without retracting the old
// ones (P2 accuracy bug found in codex review of PR #4812).
func TestMaterializeParseBoundedFilePurgesStaleEntities(t *testing.T) {
	t.Parallel()

	got, err := Materialize(Input{
		RepoID:       "repository:r_test0020",
		SourceSystem: "git",
		Files: []File{
			{
				Path:         "src/big_bundle.js",
				Language:     "javascript",
				Body:         "/* over 1 MiB, parse skipped by byte cap */",
				ParseBounded: true,
				// No EntityBuckets: the parser returned zero entities because
				// the byte cap fired before tree-sitter ran.
			},
		},
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v, want nil", err)
	}

	if got, want := len(got.Records), 1; got != want {
		t.Fatalf("record count = %d, want %d (content record must survive parse-bounded files)", got, want)
	}

	if !got.Records[0].PurgeEntities {
		t.Fatalf(
			"Records[0].PurgeEntities = false, want true for a ParseBounded file "+
				"(path=%q) so the writer retracts stale content_entities left "+
				"from a prior indexing run before the file grew past the byte cap",
			got.Records[0].Path,
		)
	}
}

// TestMaterializeNonBoundedFileDoesNotForcePurge verifies that an ordinary
// file (ParseBounded=false) is unaffected by the new signal: PurgeEntities
// stays false and its real entities materialize normally.
func TestMaterializeNonBoundedFileDoesNotForcePurge(t *testing.T) {
	t.Parallel()

	got, err := Materialize(Input{
		RepoID:       "repository:r_test0021",
		SourceSystem: "git",
		Files: []File{
			{
				Path:     "src/widget.js",
				Language: "javascript",
				Body:     "function widget() {}\n",
				EntityBuckets: map[string][]Entity{
					"functions": {{Name: "widget", LineNumber: 1}},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v, want nil", err)
	}

	if got.Records[0].PurgeEntities {
		t.Fatalf("Records[0].PurgeEntities = true, want false for a normal non-bounded file")
	}
	if got, want := len(got.Entities), 1; got != want {
		t.Fatalf("entity count = %d, want %d (normal file entities must still materialize)", got, want)
	}
}
