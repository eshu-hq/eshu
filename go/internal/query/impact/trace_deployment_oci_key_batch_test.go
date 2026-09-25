// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// testOCIRecordedMaxKeys is the bounded_key_batch max_keys that
// go/internal/queryplan/testdata/query-source-coverage.yaml records for
// fetchOCIImageTagRows, fetchOCIImagesByDigest and fetchOCIRepositoriesByUID.
//
// These tests pin that the bound is ENFORCED in code rather than assumed
// (#6590). The key sets feeding these reads are deduplicated but uncapped in
// count -- images-per-workload is unbounded -- so a large enough deployment
// trace can put more keys into one IN-list than the recorded bound allows.
// Splitting across statements must not drop a key: every key has to be asked
// for exactly once, or the answer silently loses an image.
const testOCIRecordedMaxKeys = 250

func TestFetchOCIImageRegistryTruthBatchesDigestKeysWithinRecordedBound(t *testing.T) {
	t.Parallel()

	const refs = testOCIRecordedMaxKeys + 50
	imageRefs := make([]string, 0, refs)
	for i := 0; i < refs; i++ {
		imageRefs = append(imageRefs, fmt.Sprintf("ghcr.io/acme/payments-api@sha256:%064x", i))
	}

	queried := make(map[string]int, refs)
	_, err := FetchOCIImageRegistryTruth(t.Context(), graph.FakeWorkloadGraphReader{
		RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			// Only the ContainerImage label: the closing paren keeps
			// ContainerImageIndex and ContainerImageDescriptor out.
			if !strings.Contains(cypher, "MATCH (image:ContainerImage)") {
				return nil, nil
			}
			keys, ok := params["digests"].([]string)
			if !ok {
				t.Fatalf("digests param = %T, want []string", params["digests"])
			}
			if len(keys) > testOCIRecordedMaxKeys {
				t.Errorf("one image-by-digest statement carried %d digests, want <= %d (the recorded max_keys)",
					len(keys), testOCIRecordedMaxKeys)
			}
			for _, key := range keys {
				queried[key]++
			}
			return nil, nil
		},
	}, imageRefs)
	if err != nil {
		t.Fatalf("FetchOCIImageRegistryTruth() error = %v, want nil", err)
	}
	if len(queried) != refs {
		t.Fatalf("queried %d distinct digests against ContainerImage, want %d: batching must not drop a key",
			len(queried), refs)
	}
	for key, n := range queried {
		if n != 1 {
			t.Fatalf("digest %s queried %d times against ContainerImage, want exactly 1", key, n)
		}
	}
}

func TestFetchOCIImageRegistryTruthBatchesTagRefsWithinRecordedBound(t *testing.T) {
	t.Parallel()

	const refs = testOCIRecordedMaxKeys + 50
	imageRefs := make([]string, 0, refs)
	for i := 0; i < refs; i++ {
		imageRefs = append(imageRefs, fmt.Sprintf("ghcr.io/acme/payments-api:v%d", i))
	}

	queried := make(map[string]int, refs)
	_, err := FetchOCIImageRegistryTruth(t.Context(), graph.FakeWorkloadGraphReader{
		RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			if !strings.Contains(cypher, "MATCH (tag:ContainerImageTagObservation)") {
				return nil, nil
			}
			keys, ok := params["image_refs"].([]string)
			if !ok {
				t.Fatalf("image_refs param = %T, want []string", params["image_refs"])
			}
			if len(keys) > testOCIRecordedMaxKeys {
				t.Errorf("one tag-observation statement carried %d image refs, want <= %d (the recorded max_keys)",
					len(keys), testOCIRecordedMaxKeys)
			}
			for _, key := range keys {
				queried[key]++
			}
			return nil, nil
		},
	}, imageRefs)
	if err != nil {
		t.Fatalf("FetchOCIImageRegistryTruth() error = %v, want nil", err)
	}
	if len(queried) != refs {
		t.Fatalf("queried %d distinct tag refs, want %d: batching must not drop a key", len(queried), refs)
	}
	for key, n := range queried {
		if n != 1 {
			t.Fatalf("tag ref %s queried %d times, want exactly 1", key, n)
		}
	}
}
