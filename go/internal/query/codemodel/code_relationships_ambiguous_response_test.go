// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import "testing"

// TestAmbiguousRelationshipsResponseCarriesTruncationFlags: the ambiguous
// target answer reads no neighbours, so both advertised truncation flags are
// present and false (#7151).
func TestAmbiguousRelationshipsResponseCarriesTruncationFlags(t *testing.T) {
	t.Parallel()

	resp := AmbiguousRelationshipsResponse(RelationshipsRequest{Name: "run"}, RelationshipStoryResolution{})
	for _, key := range []string{"outgoing_truncated", "incoming_truncated"} {
		got, ok := resp[key].(bool)
		if !ok {
			t.Fatalf("%s missing or not a boolean: %#v", key, resp[key])
		}
		if got {
			t.Fatalf("%s = true, want false", key)
		}
	}
}
