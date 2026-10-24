// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// referenceStoredBytes is storedBytes as it was before the metadata
// short-circuit: convert to a string, apply the policy, convert back.
func referenceStoredBytes(t *contentStoreTally, stores map[string]parser.ContentStore, path string, body []byte) []byte {
	if _, governed := stores[path]; !governed {
		return body
	}
	return []byte(t.storedBody(stores, path, string(body)))
}

// TestStoredBytesMetadataCopiesNothing pins the review nit: a metadata path
// discards the body, so storedBytes must not convert a multi-megabyte body to a
// string and back only to throw both copies away.
func TestStoredBytesMetadataCopiesNothing(t *testing.T) {
	stores := map[string]parser.ContentStore{"a/.npmrc": {Policy: parser.ContentStoreMetadata}}
	body := []byte(strings.Repeat("password: abcdefgh12345\n", 1<<16)) // about 1.5 MiB
	var tally contentStoreTally
	tally.storedBytes(stores, "a/.npmrc", body) // warm the tally's map
	allocs := testing.AllocsPerRun(20, func() {
		_ = tally.storedBytes(stores, "a/.npmrc", body)
	})
	if allocs != 0 {
		t.Fatalf("storedBytes allocated %v times per call for a metadata path, want 0 (it copied the body)", allocs)
	}
}

// TestStoredBytesMatchesTheReferenceForEveryPolicy proves the short-circuit is
// behavior-identical: same returned bytes (and non-nil, because
// docs.ReadDocumentationBodyWithError treats a nil body as "not supplied"), same
// tally outcome counts, replaced count, and failed paths, for metadata and for
// the policies that still run the redactor, over good, empty, NUL, non-UTF-8,
// and oversized bodies. A metadata path never fails, whatever the body.
func TestStoredBytesMatchesTheReferenceForEveryPolicy(t *testing.T) {
	t.Parallel()

	stores := map[string]parser.ContentStore{
		"meta":   {Policy: parser.ContentStoreMetadata},
		"redact": policyMarker("npmAuthToken"),
	}
	bodies := map[string][]byte{
		"good":      []byte("npmAuthToken: abcdef123456\n"),
		"empty":     {},
		"nil":       nil,
		"nul":       []byte("a\x00b: c\n"),
		"not utf-8": []byte("npmAuthToken: \xff\xfe\n"),
		"utf-16":    []byte("p\x00a\x00s\x00s\x00\n\x00"),
		"oversized": bytes.Repeat([]byte("a"), 5<<20),
	}
	for _, path := range []string{"meta", "redact", "ungoverned"} {
		for name, body := range bodies {
			var got, want contentStoreTally
			gotBytes := got.storedBytes(stores, path, bytes.Clone(body))
			wantBytes := referenceStoredBytes(&want, stores, path, bytes.Clone(body))
			if !bytes.Equal(gotBytes, wantBytes) {
				t.Errorf("%s/%s: stored bytes differ (len %d vs %d)", path, name, len(gotBytes), len(wantBytes))
			}
			if (gotBytes == nil) != (wantBytes == nil) {
				t.Errorf("%s/%s: nil-ness differs: got nil=%v want nil=%v", path, name, gotBytes == nil, wantBytes == nil)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s/%s: tally differs:\n got  %+v\n want %+v", path, name, got, want)
			}
		}
	}
	// The metadata policy tallies metadata/metadata even for a body that would
	// fail a redactor; the short-circuit must not change that count.
	var tally contentStoreTally
	tally.storedBytes(stores, "meta", []byte("a\x00b\xff"))
	want := map[contentStoreKey]int64{{policy: parser.ContentStoreMetadata, outcome: "metadata"}: 1}
	if !reflect.DeepEqual(tally.counts, want) || len(tally.failedPaths) != 0 {
		t.Fatalf("metadata tally = %+v failed %v, want one metadata/metadata and no failure", tally.counts, tally.failedPaths)
	}
}
