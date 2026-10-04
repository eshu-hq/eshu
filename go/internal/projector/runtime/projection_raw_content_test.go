// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type rawContentStringer string

func (value rawContentStringer) String() string { return string(value) }

func TestBuildContentRecordPreservesRawBodyAndSuppliedDigest(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		body string
	}{
		{"leading whitespace", "  def run():\n    pass\n"},
		{"trailing newline", "def run():\n    pass\n"},
		{"whitespace only", " \t\n "},
		{"empty", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fact := facts.Envelope{Payload: map[string]any{
				"content_path": "app.py", "content_body": test.body,
				"content_digest": "supplied-digest",
			}}
			record, ok := BuildContentRecord(fact)
			if !ok {
				t.Fatal("content fact was skipped")
			}
			if record.Body != test.body || record.Digest != "supplied-digest" {
				t.Fatalf("body=%q digest=%q, want body=%q and supplied digest", record.Body, record.Digest, test.body)
			}
		})
	}
}

func TestBuildContentEntityRecordPreservesSourceCacheWithPathAndTypeAliases(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		pathKey string
		typeKey string
		cache   string
	}{
		{"content path and kind", "content_path", "entity_kind", "  func run() {}\n"},
		{"relative path and type", "relative_path", "entity_type", "\n  body\n"},
		{"path and sql type", "path", "sql_entity_type", " \t\n "},
		{"empty", "path", "entity_type", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			payload := map[string]any{
				test.pathKey: "app.py", test.typeKey: "Function",
				"entity_name": "run", "source_cache": test.cache,
			}
			entity, ok := BuildContentEntityRecord("repository:r_test", facts.Envelope{Payload: payload})
			if !ok {
				t.Fatal("content entity fact was skipped")
			}
			if entity.SourceCache != test.cache {
				t.Fatalf("SourceCache=%q, want %q", entity.SourceCache, test.cache)
			}
		})
	}
}

func TestContentBuildersKeepStringerTextAndIgnoreNonStringScalars(t *testing.T) {
	t.Parallel()
	filePayload := map[string]any{
		"content_path": "app.py", "content_body": rawContentStringer("  body\n"),
	}
	file, ok := BuildContentRecord(facts.Envelope{Payload: filePayload})
	if !ok || file.Body != "  body\n" {
		t.Fatalf("stringer body = %q, %t", file.Body, ok)
	}
	filePayload["content_body"] = 42
	file, ok = BuildContentRecord(facts.Envelope{Payload: filePayload})
	if !ok || file.Body != "" {
		t.Fatalf("numeric body = %q, %t; want empty as before", file.Body, ok)
	}
	entityPayload := map[string]any{
		"content_path": "app.py", "entity_type": "Function", "entity_name": "f",
		"source_cache": rawContentStringer("  source\n"),
	}
	entity, ok := BuildContentEntityRecord("repository:r_test", facts.Envelope{Payload: entityPayload})
	if !ok || entity.SourceCache != "  source\n" {
		t.Fatalf("stringer source cache = %q, %t", entity.SourceCache, ok)
	}
	entityPayload["source_cache"] = true
	entity, ok = BuildContentEntityRecord("repository:r_test", facts.Envelope{Payload: entityPayload})
	if !ok || entity.SourceCache != "" {
		t.Fatalf("bool source cache = %q, %t; want empty as before", entity.SourceCache, ok)
	}
}
