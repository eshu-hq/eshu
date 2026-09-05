// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryauth

import (
	"reflect"
	"testing"
)

// TestNormalizeAuthContextDefaultsAndTrims pins the normalization the
// supply-chain suppression handler depends on: empty mode defaults to scoped,
// string fields are trimmed, and allow-lists are cleaned. It guards the #6060
// lane-A hoist of root's normalizeAuthContext into this leaf.
func TestNormalizeAuthContextDefaultsAndTrims(t *testing.T) {
	t.Parallel()

	got := NormalizeAuthContext(AuthContext{
		TenantID:             "  tenant-a  ",
		AllowedScopeIDs:      []string{" scope-1 ", "", "scope-1"},
		AllowedRepositoryIDs: []string{"repo://team/api", " "},
	})
	if got.Mode != AuthModeScoped {
		t.Fatalf("Mode = %q, want %q", got.Mode, AuthModeScoped)
	}
	if got.TenantID != "tenant-a" {
		t.Fatalf("TenantID = %q, want %q", got.TenantID, "tenant-a")
	}
	if want := []string{"scope-1"}; !reflect.DeepEqual(got.AllowedScopeIDs, want) {
		t.Fatalf("AllowedScopeIDs = %#v, want %#v", got.AllowedScopeIDs, want)
	}
	if want := []string{"repo://team/api"}; !reflect.DeepEqual(got.AllowedRepositoryIDs, want) {
		t.Fatalf("AllowedRepositoryIDs = %#v, want %#v", got.AllowedRepositoryIDs, want)
	}
}

// TestNormalizeAuthContextPreservesExplicitMode ensures a caller-set mode is
// never overwritten by the default.
func TestNormalizeAuthContextPreservesExplicitMode(t *testing.T) {
	t.Parallel()

	got := NormalizeAuthContext(AuthContext{Mode: AuthModeShared})
	if got.Mode != AuthModeShared {
		t.Fatalf("Mode = %q, want %q", got.Mode, AuthModeShared)
	}
}
