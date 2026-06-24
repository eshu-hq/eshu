// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "testing"

func TestEC2InstancePostureFactKindsAndSchemaVersions(t *testing.T) {
	t.Parallel()

	kinds := EC2InstancePostureFactKinds()
	wantKinds := []string{
		EC2InstancePostureFactKind,
	}
	if len(kinds) != len(wantKinds) {
		t.Fatalf("len(EC2InstancePostureFactKinds()) = %d, want %d", len(kinds), len(wantKinds))
	}
	for i, want := range wantKinds {
		if kinds[i] != want {
			t.Fatalf("EC2InstancePostureFactKinds()[%d] = %q, want %q", i, kinds[i], want)
		}
		version, ok := EC2InstancePostureSchemaVersion(want)
		if !ok || version != EC2InstancePostureSchemaVersionV1 {
			t.Fatalf("EC2InstancePostureSchemaVersion(%q) = %q, %v, want %q, true", want, version, ok, EC2InstancePostureSchemaVersionV1)
		}
	}

	if _, ok := EC2InstancePostureSchemaVersion("not_a_posture_kind"); ok {
		t.Fatalf("EC2InstancePostureSchemaVersion(unknown) ok = true, want false")
	}
}

func TestEC2InstancePostureFactKindName(t *testing.T) {
	t.Parallel()

	if EC2InstancePostureFactKind != "ec2_instance_posture" {
		t.Fatalf("EC2InstancePostureFactKind = %q, want %q", EC2InstancePostureFactKind, "ec2_instance_posture")
	}
}

func TestEC2InstancePostureFactKindsReturnsCopy(t *testing.T) {
	t.Parallel()

	kinds := EC2InstancePostureFactKinds()
	kinds[0] = "mutated"

	if got := EC2InstancePostureFactKinds()[0]; got != EC2InstancePostureFactKind {
		t.Fatalf("EC2InstancePostureFactKinds()[0] = %q, want %q", got, EC2InstancePostureFactKind)
	}
}
