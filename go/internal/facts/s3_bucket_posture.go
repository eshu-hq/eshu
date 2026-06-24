// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "slices"

const (
	// S3BucketPostureFactKind identifies one derived S3 bucket security-posture
	// fact. It is metadata-only posture evidence reported by the AWS collector:
	// block-public-access flags, default-encryption detail (SSE-KMS key ARN and
	// bucket-key state), versioning and MFA-delete state, object-ownership /
	// ACL-disabled state, access-logging target, replication presence, and
	// booleans DERIVED from the bucket policy document (public grant,
	// cross-account principal). It never carries the raw bucket policy JSON, ACL
	// grants, or object data. It is source evidence only; reducer graph
	// projection of this posture is a separate consumer.
	S3BucketPostureFactKind = "s3_bucket_posture"

	// S3BucketPostureSchemaVersionV1 is the first S3 bucket posture fact schema.
	S3BucketPostureSchemaVersionV1 = "1.0.0"
)

var s3BucketPostureFactKinds = []string{
	S3BucketPostureFactKind,
}

var s3BucketPostureSchemaVersions = map[string]string{
	S3BucketPostureFactKind: S3BucketPostureSchemaVersionV1,
}

// S3BucketPostureFactKinds returns the accepted S3 bucket posture fact kinds in
// source-contract order. The returned slice is a copy; mutating it does not
// change the registry.
func S3BucketPostureFactKinds() []string {
	return slices.Clone(s3BucketPostureFactKinds)
}

// S3BucketPostureSchemaVersion returns the schema version for an S3 bucket
// posture fact kind, and reports whether the kind is registered.
func S3BucketPostureSchemaVersion(factKind string) (string, bool) {
	version, ok := s3BucketPostureSchemaVersions[factKind]
	return version, ok
}
