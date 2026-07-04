// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"reflect"
	"testing"
)

// TestCMEKKeyFullResourceName covers the shared strict CMEK normalization that
// the Pub/Sub Topic, Secret Manager Secret, Memorystore Redis Instance, Dataflow
// Job, Filestore Instance, and Logging Log Bucket extractors all route through.
// The valid-input rows (bare relative name, leading-slash relative name, already
// Cloud KMS full name, blank/whitespace) reproduce the exact expectations of the
// per-extractor helper tests this replaces, so the convergence is proven not to
// change behavior for any input real Cloud Asset Inventory emits. The
// wrong-domain row asserts the strict contract: an absolute name for a non-KMS
// service is rejected so it can never poison a KMS anchor or edge.
func TestCMEKKeyFullResourceName(t *testing.T) {
	const kmsFull = "//cloudkms.googleapis.com/projects/p/locations/l/keyRings/r/cryptoKeys/k"
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"relative key", "projects/p/locations/l/keyRings/r/cryptoKeys/k", kmsFull},
		{"leading slash", "/projects/p/locations/l/keyRings/r/cryptoKeys/k", kmsFull},
		{"already kms full name", kmsFull, kmsFull},
		{"wrong-domain absolute name rejected", "//compute.googleapis.com/projects/p/whatever", ""},
		{"wrong-domain pubsub absolute name rejected", "//pubsub.googleapis.com/projects/p/topics/t", ""},
		{"whitespace only", "   ", ""},
		{"blank", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cmekKeyFullResourceName(tc.in); got != tc.want {
				t.Errorf("cmekKeyFullResourceName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestDedupeSortedNonEmpty proves the shared sorted-dedup helper trims, drops
// blanks, deduplicates, and sorts — the contract the BigQuery Dataset access
// summary and Firebase Ruleset service list previously relied on via the deleted
// stringSet type, and the Pub/Sub Topic message-storage regions rely on directly.
func TestDedupeSortedNonEmpty(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"nil", nil, nil},
		{"all blank", []string{"", "  ", "\t"}, nil},
		{
			"sorts trims dedupes",
			[]string{"c", " a ", "b", "a", ""},
			[]string{"a", "b", "c"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := dedupeSortedNonEmpty(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("dedupeSortedNonEmpty(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
