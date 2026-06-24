// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "testing"

func TestS3ExternalPrincipalGrantFactKindRegistry(t *testing.T) {
	kinds := S3ExternalPrincipalGrantFactKinds()
	want := []string{S3ExternalPrincipalGrantFactKind}
	if len(kinds) != len(want) {
		t.Fatalf("len(S3ExternalPrincipalGrantFactKinds()) = %d, want %d", len(kinds), len(want))
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("S3ExternalPrincipalGrantFactKinds()[%d] = %q, want %q", i, kinds[i], want[i])
		}
		version, ok := S3ExternalPrincipalGrantSchemaVersion(kinds[i])
		if !ok {
			t.Fatalf("S3ExternalPrincipalGrantSchemaVersion(%q) ok = false", kinds[i])
		}
		if version != S3ExternalPrincipalGrantSchemaVersionV1 {
			t.Fatalf("S3ExternalPrincipalGrantSchemaVersion(%q) = %q, want %q", kinds[i], version, S3ExternalPrincipalGrantSchemaVersionV1)
		}
	}

	if _, ok := S3ExternalPrincipalGrantSchemaVersion("nope"); ok {
		t.Fatalf("S3ExternalPrincipalGrantSchemaVersion(unknown) ok = true, want false")
	}

	kinds[0] = "mutated"
	if got := S3ExternalPrincipalGrantFactKinds()[0]; got != S3ExternalPrincipalGrantFactKind {
		t.Fatalf("S3ExternalPrincipalGrantFactKinds returned mutable backing slice, got first kind %q", got)
	}
}

func TestS3ExternalPrincipalGrantFactKindValue(t *testing.T) {
	if S3ExternalPrincipalGrantFactKind != "s3_external_principal_grant" {
		t.Fatalf("S3ExternalPrincipalGrantFactKind = %q, want %q", S3ExternalPrincipalGrantFactKind, "s3_external_principal_grant")
	}
}
