// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "testing"

func TestRDSPostureFactKindsAndSchemaVersions(t *testing.T) {
	t.Parallel()

	kinds := RDSPostureFactKinds()
	wantKinds := []string{
		RDSInstancePostureFactKind,
	}
	if len(kinds) != len(wantKinds) {
		t.Fatalf("len(RDSPostureFactKinds()) = %d, want %d", len(kinds), len(wantKinds))
	}
	for i, want := range wantKinds {
		if kinds[i] != want {
			t.Fatalf("RDSPostureFactKinds()[%d] = %q, want %q", i, kinds[i], want)
		}
		version, ok := RDSPostureSchemaVersion(want)
		if !ok || version != RDSPostureSchemaVersionV1 {
			t.Fatalf("RDSPostureSchemaVersion(%q) = %q, %v, want %q, true", want, version, ok, RDSPostureSchemaVersionV1)
		}
	}

	if _, ok := RDSPostureSchemaVersion("not_a_posture_kind"); ok {
		t.Fatalf("RDSPostureSchemaVersion(unknown) ok = true, want false")
	}
}

func TestRDSInstancePostureFactKindName(t *testing.T) {
	t.Parallel()

	if RDSInstancePostureFactKind != "rds_instance_posture" {
		t.Fatalf("RDSInstancePostureFactKind = %q, want %q", RDSInstancePostureFactKind, "rds_instance_posture")
	}
}

func TestRDSPostureFactKindsReturnsCopy(t *testing.T) {
	t.Parallel()

	kinds := RDSPostureFactKinds()
	kinds[0] = "mutated"

	if got := RDSPostureFactKinds()[0]; got != RDSInstancePostureFactKind {
		t.Fatalf("RDSPostureFactKinds()[0] = %q, want %q", got, RDSInstancePostureFactKind)
	}
}
