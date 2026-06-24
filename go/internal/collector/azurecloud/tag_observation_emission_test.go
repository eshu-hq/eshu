// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azurecloud

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestCollectEmitsTagObservationsWhenKeyed proves a keyed collector emits one
// azure_tag_observation fact per tagged resource, alongside the resource fact,
// with fingerprinted (never raw) tag values.
func TestCollectEmitsTagObservationsWhenKeyed(t *testing.T) {
	provider := newTwoPageProvider(t)
	key := testRedactionKey(t)

	result, err := NewCollector(provider, nil, WithRedactionKey(key)).
		Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Collect error: %v", err)
	}

	tagFacts := factsOfKind(result.Facts, facts.AzureTagObservationFactKind)
	if len(tagFacts) == 0 {
		t.Fatal("expected azure_tag_observation facts for tagged resources")
	}
	if result.TagObservationCount != len(tagFacts) {
		t.Fatalf("TagObservationCount = %d, want %d", result.TagObservationCount, len(tagFacts))
	}
	// One tag observation per tagged resource; never more than the resources.
	if result.TagObservationCount > result.ResourceCount {
		t.Fatalf("tag observations %d exceed resources %d", result.TagObservationCount, result.ResourceCount)
	}
	for _, f := range tagFacts {
		fps, ok := f.Payload["tag_value_fingerprints"].(map[string]string)
		if !ok || len(fps) == 0 {
			t.Fatalf("tag fact missing fingerprints: %#v", f.Payload["tag_value_fingerprints"])
		}
		for k, marker := range fps {
			if marker == "prod" || marker == "platform" {
				t.Fatalf("raw tag value leaked for key %q: %q", k, marker)
			}
		}
	}
}

// TestCollectSkipsTagObservationsWithoutKey proves the collector fails closed:
// with no redaction key it emits no azure_tag_observation facts (tag values
// must never be fingerprinted, or carried, without a key).
func TestCollectSkipsTagObservationsWithoutKey(t *testing.T) {
	provider := newTwoPageProvider(t)

	result, err := NewCollector(provider, nil).Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Collect error: %v", err)
	}
	if n := len(factsOfKind(result.Facts, facts.AzureTagObservationFactKind)); n != 0 {
		t.Fatalf("expected no tag observations without a redaction key, got %d", n)
	}
	if result.TagObservationCount != 0 {
		t.Fatalf("TagObservationCount = %d, want 0 without a key", result.TagObservationCount)
	}
	// Resource facts are still emitted regardless of the tag-observation key.
	if result.ResourceCount == 0 {
		t.Fatal("expected resource facts even without a redaction key")
	}
}
