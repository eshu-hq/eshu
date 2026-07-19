// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content/shape"
	jsonparser "github.com/eshu-hq/eshu/go/internal/parser/json"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// TestDerivedJSONRowsMaterializeToPositionalLineOne is the end-to-end proof
// for issue #5358's P1#2 finding: warehouse_replay.json/governance_replay.json
// rows (data_intelligence.go/governance.go) describe an external system, not
// one JSON source token, so no real source line exists for them. #5329 made
// the parser OMIT line_number for these rows on the theory that this would
// leave materialized entities with no real source line -- that theory was
// wrong. entityBucketsFromParsed's snapshotPayloadInt defaults an absent
// "line_number" to 0, and shape.indexedEntity.lineNumber() (materialize_labels.go)
// coerces any LineNumber < 1 back to 1 before it is hashed into
// content.CanonicalEntityID and persisted as content.EntityRecord.StartLine.
// This test proves that coercion fires regardless of what the parser payload
// says: the materialized entity ends up at StartLine 1 either way, so
// omitting line_number changed nothing observable downstream -- it only made
// the parser payload claim (falsely) that no line existed. Threading a
// genuine "no source line" sentinel through shape.Entity/
// content.EntityRecord.StartLine (a NOT NULL Postgres column read by dozens
// of query/export/UI call sites across every language parser) was assessed
// as disproportionate for this fix, so the parser now states the pre-#5329
// positional line_number: 1 explicitly for these rows -- the honest version
// of the value they get regardless, not a claim of an accuracy fix that
// never took effect.
//
// This test runs the real JSON parser and the real production bucket
// extraction (entityBucketsFromParsed) and shape.Materialize, so it pins the
// actual end-to-end behavior rather than only the parser-payload layer.
func TestDerivedJSONRowsMaterializeToPositionalLineOne(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "warehouse_replay.json")
	body := `{
  "metadata": {"workspace": "analytics"},
  "assets": [
    {
      "database": "raw",
      "schema": "public",
      "name": "orders",
      "kind": "table",
      "columns": [{"name": "id"}]
    }
  ],
  "query_history": [
    {"query_id": "q1", "name": "daily_load", "touched_assets": ["orders"]}
  ]
}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write warehouse_replay.json fixture: %v", err)
	}

	payload, err := jsonparser.Parse(path, false, shared.Options{}, jsonparser.Config{})
	if err != nil {
		t.Fatalf("jsonparser.Parse() error = %v, want nil", err)
	}

	buckets := entityBucketsFromParsed(payload)
	dataAssets, ok := buckets["data_assets"]
	if !ok || len(dataAssets) == 0 {
		t.Fatalf("entityBucketsFromParsed()[\"data_assets\"] = %#v, want at least one entity", buckets["data_assets"])
	}

	materialization, err := shape.Materialize(shape.Input{
		RepoID: "test-repo",
		Files: []shape.File{
			{
				Path:          "warehouse_replay.json",
				EntityBuckets: buckets,
			},
		},
	})
	if err != nil {
		t.Fatalf("shape.Materialize() error = %v, want nil", err)
	}

	found := false
	for _, entity := range materialization.Entities {
		if entity.EntityType != "DataAsset" {
			continue
		}
		found = true
		// shape.indexedEntity.lineNumber() coerces LineNumber < 1 to 1 --
		// content_writer.go's Write() separately hard-rejects StartLine <= 0,
		// but that guard is never reached from this path because the
		// coercion above already normalizes to 1 first.
		if entity.StartLine != 1 {
			t.Errorf("DataAsset entity %q StartLine = %d, want 1", entity.EntityName, entity.StartLine)
		}
	}
	if !found {
		t.Fatalf("materialization.Entities = %#v, want at least one DataAsset entity", materialization.Entities)
	}
}
