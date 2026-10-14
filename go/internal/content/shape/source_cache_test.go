// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shape

import (
	"maps"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestMaterializeLimitsOversizedVariableSourceCache(t *testing.T) {
	t.Parallel()

	oversizedSource := strings.Repeat("const generated = 'payload';\n", 400)
	got, err := Materialize(Input{
		RepoID: "repository:r_12345678",
		Files: []File{{
			Path:     "assets/generated.js",
			Body:     oversizedSource,
			Language: "javascript",
			EntityBuckets: map[string][]Entity{
				"variables": {{
					Name:       "generated",
					LineNumber: 1,
					EndLine:    400,
					Source:     oversizedSource,
				}},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v, want nil", err)
	}
	if len(got.Entities) != 1 {
		t.Fatalf("len(Materialize().Entities) = %d, want 1", len(got.Entities))
	}

	entity := got.Entities[0]
	if len(entity.SourceCache) > entitySourceCacheByteLimits["Variable"] {
		t.Fatalf("SourceCache length = %d, want <= %d", len(entity.SourceCache), entitySourceCacheByteLimits["Variable"])
	}
	if entity.Metadata[sourceCacheTruncatedMetadataKey] != true {
		t.Fatalf("Metadata[%s] = %#v, want true", sourceCacheTruncatedMetadataKey, entity.Metadata[sourceCacheTruncatedMetadataKey])
	}
	if got, want := entity.Metadata[sourceCacheOriginalBytesMetadataKey], len(oversizedSource); got != want {
		t.Fatalf("Metadata[%s] = %#v, want %#v", sourceCacheOriginalBytesMetadataKey, got, want)
	}
	if got, want := entity.Metadata[sourceCacheLimitBytesMetadataKey], entitySourceCacheByteLimits["Variable"]; got != want {
		t.Fatalf("Metadata[%s] = %#v, want %#v", sourceCacheLimitBytesMetadataKey, got, want)
	}
}

func TestMaterializeLimitsOversizedSqlColumnSourceCache(t *testing.T) {
	t.Parallel()

	limit, ok := entitySourceCacheByteLimits["SqlColumn"]
	if !ok {
		t.Fatalf("entitySourceCacheByteLimits has no SqlColumn entry (issue #8001)")
	}

	oversizedBody := strings.Repeat("  big_col TEXT DEFAULT 'padding to widen the row',\n", 400)
	got, err := Materialize(Input{
		RepoID: "repository:r_sqlcolumn",
		Files: []File{{
			Path:     "schema.sql",
			Body:     oversizedBody,
			Language: "sql",
			EntityBuckets: map[string][]Entity{
				"sql_columns": {{
					Name:       "public.events.big_col",
					LineNumber: 1,
					EndLine:    400,
				}},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v, want nil", err)
	}
	if len(got.Entities) != 1 {
		t.Fatalf("len(Materialize().Entities) = %d, want 1", len(got.Entities))
	}

	entity := got.Entities[0]
	if len(entity.SourceCache) > limit {
		t.Fatalf("SourceCache length = %d, want <= %d", len(entity.SourceCache), limit)
	}
	if entity.Metadata[sourceCacheTruncatedMetadataKey] != true {
		t.Fatalf("Metadata[%s] = %#v, want true", sourceCacheTruncatedMetadataKey, entity.Metadata[sourceCacheTruncatedMetadataKey])
	}
	if got, want := entity.Metadata[sourceCacheOriginalBytesMetadataKey], len(oversizedBody); got != want {
		t.Fatalf("Metadata[%s] = %#v, want %#v", sourceCacheOriginalBytesMetadataKey, got, want)
	}
	if got, want := entity.Metadata[sourceCacheLimitBytesMetadataKey], limit; got != want {
		t.Fatalf("Metadata[%s] = %#v, want %#v", sourceCacheLimitBytesMetadataKey, got, want)
	}
}

func TestMaterializeLimitsOversizedSqlSiblingSourceCache(t *testing.T) {
	t.Parallel()

	siblings := []struct {
		bucket string
		label  string
		name   string
	}{
		{bucket: "sql_tables", label: "SqlTable", name: "public.events"},
		{bucket: "sql_views", label: "SqlView", name: "public.events_view"},
		{bucket: "sql_functions", label: "SqlFunction", name: "public.archive_events"},
		{bucket: "sql_triggers", label: "SqlTrigger", name: "public.events_trigger"},
		{bucket: "sql_indexes", label: "SqlIndex", name: "public.events_idx"},
		{bucket: "sql_migrations", label: "SqlMigration", name: "20241014000000_create_events"},
	}

	for _, sibling := range siblings {
		t.Run(sibling.label, func(t *testing.T) {
			t.Parallel()

			limit, ok := entitySourceCacheByteLimits[sibling.label]
			if !ok {
				t.Fatalf("entitySourceCacheByteLimits has no %s entry (issue #8071)", sibling.label)
			}

			oversizedBody := strings.Repeat("  big_col TEXT DEFAULT 'padding to widen the row',\n", 400)
			got, err := Materialize(Input{
				RepoID: "repository:r_sqlsibling",
				Files: []File{{
					Path:     "schema.sql",
					Body:     oversizedBody,
					Language: "sql",
					EntityBuckets: map[string][]Entity{
						sibling.bucket: {{
							Name:       sibling.name,
							LineNumber: 1,
							EndLine:    400,
						}},
					},
				}},
			})
			if err != nil {
				t.Fatalf("Materialize() error = %v, want nil", err)
			}
			if len(got.Entities) != 1 {
				t.Fatalf("len(Materialize().Entities) = %d, want 1", len(got.Entities))
			}

			entity := got.Entities[0]
			if got, want := entity.EntityType, sibling.label; got != want {
				t.Fatalf("EntityType = %q, want %q", got, want)
			}
			// Census assertion: max source_cache bytes for this SQL label
			// must not exceed its cap-table limit.
			if len(entity.SourceCache) > limit {
				t.Fatalf("%s SourceCache length = %d, want <= %d", sibling.label, len(entity.SourceCache), limit)
			}
			if entity.Metadata[sourceCacheTruncatedMetadataKey] != true {
				t.Fatalf("%s Metadata[%s] = %#v, want true", sibling.label, sourceCacheTruncatedMetadataKey, entity.Metadata[sourceCacheTruncatedMetadataKey])
			}
			if got, want := entity.Metadata[sourceCacheOriginalBytesMetadataKey], len(oversizedBody); got != want {
				t.Fatalf("%s Metadata[%s] = %#v, want %#v", sibling.label, sourceCacheOriginalBytesMetadataKey, got, want)
			}
			if got, want := entity.Metadata[sourceCacheLimitBytesMetadataKey], limit; got != want {
				t.Fatalf("%s Metadata[%s] = %#v, want %#v", sibling.label, sourceCacheLimitBytesMetadataKey, got, want)
			}
		})
	}
}

func TestLimitEntitySourceCacheKeepsUTF8Valid(t *testing.T) {
	t.Parallel()

	source := strings.Repeat("π", entitySourceCacheByteLimits["Variable"])
	got, metadata := limitEntitySourceCache("Variable", "", source, nil)

	if !utf8.ValidString(got) {
		t.Fatalf("limited source cache is not valid UTF-8")
	}
	if len(got) > entitySourceCacheByteLimits["Variable"] {
		t.Fatalf("limited source cache length = %d, want <= %d", len(got), entitySourceCacheByteLimits["Variable"])
	}
	if metadata[sourceCacheTruncatedMetadataKey] != true {
		t.Fatalf("metadata truncation flag = %#v, want true", metadata[sourceCacheTruncatedMetadataKey])
	}
}

func TestLimitEntitySourceCacheLeavesFunctionBodiesUnchanged(t *testing.T) {
	t.Parallel()

	source := strings.Repeat("func generated() {}\n", 400)
	got, metadata := limitEntitySourceCache("Function", "", source, nil)

	if got != source {
		t.Fatalf("Function source cache was changed")
	}
	if metadata != nil {
		t.Fatalf("metadata = %#v, want nil", metadata)
	}
}

func TestMaterializeLimitsWorkflowSourceCacheAtExactUTF8SafeBoundary(t *testing.T) {
	t.Parallel()

	const limit = githubActionsWorkflowSourceCacheByteLimit
	tests := []struct {
		name          string
		body          string
		wantBytes     int
		wantTruncated bool
	}{
		{name: "exact boundary", body: strings.Repeat("a", limit), wantBytes: limit},
		{name: "one byte over", body: strings.Repeat("a", limit+1), wantBytes: limit, wantTruncated: true},
		{name: "utf8 boundary", body: strings.Repeat("a", limit-1) + "π", wantBytes: limit - 1, wantTruncated: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := Input{RepoID: "repository:workflow-limit", Files: []File{{
				Path: ".github/workflows/ci.yml",
				Body: test.body,
			}}}
			first, err := Materialize(input)
			if err != nil {
				t.Fatalf("Materialize() error = %v, want nil", err)
			}
			second, err := Materialize(input)
			if err != nil {
				t.Fatalf("repeat Materialize() error = %v, want nil", err)
			}
			entity := first.Entities[0]
			if got := len(entity.SourceCache); got != test.wantBytes {
				t.Fatalf("SourceCache bytes = %d, want %d", got, test.wantBytes)
			}
			if !utf8.ValidString(entity.SourceCache) {
				t.Fatal("SourceCache is not valid UTF-8")
			}
			if got := entity.Metadata[sourceCacheTruncatedMetadataKey] == true; got != test.wantTruncated {
				t.Fatalf("truncation metadata = %t, want %t; metadata = %#v", got, test.wantTruncated, entity.Metadata)
			}
			if test.wantTruncated {
				if got, want := entity.Metadata[sourceCacheOriginalBytesMetadataKey], len(test.body); got != want {
					t.Fatalf("original bytes = %#v, want %d", got, want)
				}
				if got, want := entity.Metadata[sourceCacheLimitBytesMetadataKey], limit; got != want {
					t.Fatalf("limit bytes = %#v, want %d", got, want)
				}
			}
			if got, want := second.Entities[0].SourceCache, entity.SourceCache; got != want {
				t.Fatalf("repeat SourceCache differs")
			}
			if got, want := second.Entities[0].Metadata, entity.Metadata; !maps.Equal(got, want) {
				t.Fatalf("repeat metadata = %#v, want %#v", got, want)
			}
		})
	}
}
