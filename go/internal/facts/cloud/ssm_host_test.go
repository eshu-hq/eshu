// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloud

import "testing"

func TestSSMHostFactKindRegistry(t *testing.T) {
	kinds := SSMHostFactKinds()
	want := []string{SSMHostApplicationFactKind, SSMHostPlatformFactKind}
	if len(kinds) != len(want) {
		t.Fatalf("len(SSMHostFactKinds()) = %d, want %d", len(kinds), len(want))
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("SSMHostFactKinds()[%d] = %q, want %q", i, kinds[i], want[i])
		}
		version, ok := SSMHostSchemaVersion(kinds[i])
		if !ok {
			t.Fatalf("SSMHostSchemaVersion(%q) ok = false", kinds[i])
		}
		if version != SSMHostSchemaVersionV1 {
			t.Fatalf("SSMHostSchemaVersion(%q) = %q, want %q", kinds[i], version, SSMHostSchemaVersionV1)
		}
	}

	if _, ok := SSMHostSchemaVersion("nope"); ok {
		t.Fatalf("SSMHostSchemaVersion(unknown) ok = true, want false")
	}

	kinds[0] = "mutated"
	if got := SSMHostFactKinds()[0]; got != SSMHostApplicationFactKind {
		t.Fatalf("SSMHostFactKinds returned mutable backing slice, got first kind %q", got)
	}
}

func TestSSMHostFactKindValues(t *testing.T) {
	if SSMHostApplicationFactKind != "aws_ssm_host_application" {
		t.Fatalf("SSMHostApplicationFactKind = %q, want %q", SSMHostApplicationFactKind, "aws_ssm_host_application")
	}
	if SSMHostPlatformFactKind != "aws_ssm_host_platform" {
		t.Fatalf("SSMHostPlatformFactKind = %q, want %q", SSMHostPlatformFactKind, "aws_ssm_host_platform")
	}
	if SSMHostSchemaVersionV1 != "1.0.0" {
		t.Fatalf("SSMHostSchemaVersionV1 = %q, want %q", SSMHostSchemaVersionV1, "1.0.0")
	}
}
