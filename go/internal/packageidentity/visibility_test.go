// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packageidentity

import "testing"

func TestVisibilityConstantsMatchCollectorWireSpelling(t *testing.T) {
	t.Parallel()

	if string(VisibilityUnknown) != "unknown" {
		t.Fatalf("VisibilityUnknown = %q, want unknown", string(VisibilityUnknown))
	}
	if string(VisibilityPublic) != "public" {
		t.Fatalf("VisibilityPublic = %q, want public", string(VisibilityPublic))
	}
	if string(VisibilityPrivate) != "private" {
		t.Fatalf("VisibilityPrivate = %q, want private", string(VisibilityPrivate))
	}
}
