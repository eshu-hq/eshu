// Package s3 maps Amazon S3 bucket metadata into AWS cloud collector facts.
//
// The scanner emits reported-confidence bucket resources, a derived
// metadata-only s3_bucket_posture fact per bucket, plus relationships for
// server-access-log delivery targets. The posture fact carries block-public-
// access flags, default-encryption detail (SSE-KMS key ARN and bucket-key
// state), versioning and MFA-delete state, object-ownership / ACL-disabled
// state, access-logging target, replication presence, and booleans DERIVED
// from the bucket policy document (public grant, cross-account principal).
// Object inventory, the raw bucket policy JSON, ACL grants, replication rule
// detail, lifecycle rules, notification configuration, and mutation APIs stay
// outside this package contract.
package s3
