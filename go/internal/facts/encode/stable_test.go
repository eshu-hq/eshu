// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package encode

import (
	"testing"
	"time"
)

// TestStableIDPinsDerivationBytes pins StableID's output to digests computed
// outside Go, from the documented derivation: canonical JSON of
// {"fact_type": <type>, "identity": <normalized identity>} with sorted object
// keys and no insignificant whitespace, then SHA-256, hex-lowercase.
//
// These ids are persisted graph identity. A change in the hashed bytes
// orphans every node already written under the old id, so the derivation is a
// frozen contract and not an implementation detail. Moving this function out
// of the facts root (issue #6776) must not perturb it, and an independently
// derived expectation is what proves that -- a same-input/same-output check
// against the moved code would pass even if both sides drifted together.
func TestStableIDPinsDerivationBytes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		factType string
		identity map[string]any
		want     string
	}{
		{
			name:     "documentation section identity",
			factType: "documentation_section",
			identity: map[string]any{
				"document_id": "doc:confluence:12345",
				"revision_id": "17",
				"section_id":  "section:deployment",
			},
			want: "c02b58aad94108e01009dae9e17f789aa644aafa58d28780cbc803aa3d3da6fc",
		},
		{
			name:     "aws resource identity",
			factType: "aws_resource",
			identity: map[string]any{
				"account_id": "111122223333",
				"arn":        "arn:aws:s3:::example-bucket",
				"region":     "us-east-1",
			},
			want: "0d30a784e7a8eeefa058bc98a048eb56659409ad2cf2bc18284c2da969548301",
		},
		{
			name:     "empty identity still yields an id",
			factType: "empty_identity",
			identity: map[string]any{},
			want:     "0d9d6eec7c0474da6346f8c0827ac4813a7846b2d667af98676a1dd156f8db62",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := StableID(test.factType, test.identity); got != test.want {
				t.Fatalf("StableID(%q, ...) = %q, want %q", test.factType, got, test.want)
			}
		})
	}
}

// TestStableIDKeyOrderIsIrrelevant proves the identity map's Go iteration
// order cannot leak into the id: the same key/value set built in a different
// literal order hashes identically, because encoding/json sorts map keys.
func TestStableIDKeyOrderIsIrrelevant(t *testing.T) {
	t.Parallel()

	first := StableID("k", map[string]any{"a": "1", "b": "2", "c": "3"})
	second := StableID("k", map[string]any{"c": "3", "b": "2", "a": "1"})
	if first != second {
		t.Fatalf("StableID is order sensitive: %q != %q", first, second)
	}
}

// TestStableIDNormalizesTimeAndSliceIdentity covers the normalization the
// derivation applies before hashing: a time.Time is reduced to its UTC
// RFC3339Nano form, so the same instant expressed in a different zone yields
// one id, and a []string is widened to []any so it hashes the same as the
// equivalent []any a decoded payload produces.
func TestStableIDNormalizesTimeAndSliceIdentity(t *testing.T) {
	t.Parallel()

	instant := time.Date(2026, 9, 22, 15, 4, 5, 123456789, time.UTC)
	shifted := instant.In(time.FixedZone("plus2", 2*60*60))
	if got, want := StableID("k", map[string]any{"at": shifted}), StableID("k", map[string]any{"at": instant}); got != want {
		t.Fatalf("StableID did not normalize a zoned time: %q != %q", got, want)
	}

	typed := StableID("k", map[string]any{"tags": []string{"x", "y"}})
	decoded := StableID("k", map[string]any{"tags": []any{"x", "y"}})
	if typed != decoded {
		t.Fatalf("StableID([]string) = %q, want the []any form %q", typed, decoded)
	}
}

// TestStableIDDistinguishesFactType proves the fact type is part of the
// hashed input, so two families cannot collide on a shared identity shape.
func TestStableIDDistinguishesFactType(t *testing.T) {
	t.Parallel()

	identity := map[string]any{"id": "shared"}
	if StableID("family_one", identity) == StableID("family_two", identity) {
		t.Fatal("StableID collides across fact types for one identity")
	}
}
