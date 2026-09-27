// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oci

import (
	"reflect"
	"testing"
)

const testDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"

func TestImageRefDigest(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"ghcr.io/acme/x@" + testDigest: testDigest,
		"ghcr.io/acme/x:latest":        "",
		"ghcr.io/acme/x@sha256:short":  "",
		"":                             "",
	}
	for ref, want := range cases {
		if got := ImageRefDigest(ref); got != want {
			t.Errorf("ImageRefDigest(%q) = %q, want %q", ref, got, want)
		}
	}
}

func TestSplitImageRefsPartitionsAndDedupes(t *testing.T) {
	t.Parallel()
	digestRef := "ghcr.io/acme/x@" + testDigest
	digestRefs, tagRefs := SplitImageRefs([]string{
		digestRef, digestRef, "ghcr.io/acme/y:latest", "ghcr.io/acme/a:latest", "",
	})
	if want := []string{digestRef}; !reflect.DeepEqual(digestRefs[testDigest], want) {
		t.Fatalf("digestRefs[%q] = %#v, want %#v (dedup)", testDigest, digestRefs[testDigest], want)
	}
	if want := []string{"ghcr.io/acme/a:latest", "ghcr.io/acme/y:latest"}; !reflect.DeepEqual(tagRefs, want) {
		t.Fatalf("tagRefs = %#v, want %#v (sorted)", tagRefs, want)
	}
}

func TestBuildDigestTruthRows(t *testing.T) {
	t.Parallel()
	imageRef := "ghcr.io/acme/x@" + testDigest
	rows := []map[string]any{{
		"image_id": "img-1", "digest": testDigest, "repository_id": "repo-1",
		"registry": "ghcr.io", "repository": "acme/x", "provider": "ghcr", "media_type": "application/vnd.oci.image.manifest.v1+json",
	}}
	truth := BuildDigestTruthRows(rows, map[string][]string{testDigest: {imageRef}})
	if len(truth) != 1 {
		t.Fatalf("BuildDigestTruthRows() = %#v, want 1 row", truth)
	}
	if got := truth[0]["image_ref"]; got != imageRef {
		t.Errorf("image_ref = %#v, want %q", got, imageRef)
	}
	if got := truth[0]["ambiguous"]; got != false {
		t.Errorf("ambiguous = %#v, want false", got)
	}
}

func TestBuildTagTruthRowsResolvesAndDisambiguates(t *testing.T) {
	t.Parallel()
	otherDigest := "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	resolved := "ghcr.io/acme/resolved:latest"
	ambiguous := "ghcr.io/acme/ambiguous:latest"

	truth := BuildTagTruthRows([]map[string]any{
		{"image_ref": resolved, "digest": testDigest, "tag": "latest", "repository_id": "repo-1", "registry": "ghcr.io", "repository": "acme/resolved"},
		{"image_ref": ambiguous, "digest": testDigest, "tag": "latest", "repository_id": "repo-2", "registry": "ghcr.io", "repository": "acme/ambiguous"},
		{"image_ref": ambiguous, "digest": otherDigest, "tag": "latest", "repository_id": "repo-2", "registry": "ghcr.io", "repository": "acme/ambiguous"},
	})
	if len(truth) != 2 {
		t.Fatalf("BuildTagTruthRows() = %#v, want 2 rows", truth)
	}
	byRef := make(map[string]map[string]any, len(truth))
	for _, row := range truth {
		byRef[row["image_ref"].(string)] = row
	}
	if got := byRef[resolved]["match_strength"]; got != TagMatchStrength {
		t.Errorf("resolved match_strength = %#v, want %q", got, TagMatchStrength)
	}
	if got := byRef[ambiguous]["match_strength"]; got != AmbiguousMatchStrength {
		t.Errorf("ambiguous match_strength = %#v, want %q", got, AmbiguousMatchStrength)
	}
	candidates, _ := byRef[ambiguous]["digest_candidates"].([]string)
	if len(candidates) != 2 {
		t.Fatalf("digest_candidates = %#v, want 2 candidates", byRef[ambiguous]["digest_candidates"])
	}
}

func TestIndexImagesByDigestKeepsFirstSeen(t *testing.T) {
	t.Parallel()
	first := map[string]any{"digest": testDigest, "image_id": "first"}
	second := map[string]any{"digest": testDigest, "image_id": "second"}
	indexed := IndexImagesByDigest([]map[string]any{first, second})
	if got := indexed[testDigest]["image_id"]; got != "first" {
		t.Fatalf("indexed[%q] = %#v, want the first-seen row", testDigest, indexed[testDigest])
	}
}

func TestSortedMapKeys(t *testing.T) {
	t.Parallel()
	got := SortedMapKeys(map[string]int{"b": 1, "a": 2})
	if want := []string{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SortedMapKeys() = %#v, want %#v", got, want)
	}
}
