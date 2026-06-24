// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "testing"

func TestS3BucketPostureFactKindRegistry(t *testing.T) {
	kinds := S3BucketPostureFactKinds()
	want := []string{S3BucketPostureFactKind}
	if len(kinds) != len(want) {
		t.Fatalf("len(S3BucketPostureFactKinds()) = %d, want %d", len(kinds), len(want))
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("S3BucketPostureFactKinds()[%d] = %q, want %q", i, kinds[i], want[i])
		}
		version, ok := S3BucketPostureSchemaVersion(kinds[i])
		if !ok {
			t.Fatalf("S3BucketPostureSchemaVersion(%q) ok = false", kinds[i])
		}
		if version != S3BucketPostureSchemaVersionV1 {
			t.Fatalf("S3BucketPostureSchemaVersion(%q) = %q, want %q", kinds[i], version, S3BucketPostureSchemaVersionV1)
		}
	}

	if _, ok := S3BucketPostureSchemaVersion("nope"); ok {
		t.Fatalf("S3BucketPostureSchemaVersion(unknown) ok = true, want false")
	}

	kinds[0] = "mutated"
	if got := S3BucketPostureFactKinds()[0]; got != S3BucketPostureFactKind {
		t.Fatalf("S3BucketPostureFactKinds returned mutable backing slice, got first kind %q", got)
	}
}

func TestS3BucketPostureFactKindValue(t *testing.T) {
	if S3BucketPostureFactKind != "s3_bucket_posture" {
		t.Fatalf("S3BucketPostureFactKind = %q, want %q", S3BucketPostureFactKind, "s3_bucket_posture")
	}
}
