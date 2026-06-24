// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "testing"

func TestSecurityAlertFactKindsAndSchemaVersions(t *testing.T) {
	t.Parallel()

	kinds := SecurityAlertFactKinds()
	wantKinds := []string{
		SecurityAlertRepositoryAlertFactKind,
	}
	if len(kinds) != len(wantKinds) {
		t.Fatalf("len(SecurityAlertFactKinds()) = %d, want %d", len(kinds), len(wantKinds))
	}
	for i, want := range wantKinds {
		if kinds[i] != want {
			t.Fatalf("SecurityAlertFactKinds()[%d] = %q, want %q", i, kinds[i], want)
		}
		version, ok := SecurityAlertSchemaVersion(want)
		if !ok || version != SecurityAlertSchemaVersionV1 {
			t.Fatalf("SecurityAlertSchemaVersion(%q) = %q, %v, want %q, true", want, version, ok, SecurityAlertSchemaVersionV1)
		}
	}
}

func TestSecurityAlertFactKindsReturnsCopy(t *testing.T) {
	t.Parallel()

	kinds := SecurityAlertFactKinds()
	kinds[0] = "mutated"

	if got := SecurityAlertFactKinds()[0]; got != SecurityAlertRepositoryAlertFactKind {
		t.Fatalf("SecurityAlertFactKinds()[0] = %q, want %q", got, SecurityAlertRepositoryAlertFactKind)
	}
}
