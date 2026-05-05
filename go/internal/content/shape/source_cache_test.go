package shape

import (
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
	got, metadata := limitEntitySourceCache("Variable", source, nil)

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
	got, metadata := limitEntitySourceCache("Function", source, nil)

	if got != source {
		t.Fatalf("Function source cache was changed")
	}
	if metadata != nil {
		t.Fatalf("metadata = %#v, want nil", metadata)
	}
}
