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
