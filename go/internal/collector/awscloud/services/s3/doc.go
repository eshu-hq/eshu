// Package s3 maps Amazon S3 bucket metadata into AWS cloud collector facts.
//
// The scanner emits reported-confidence bucket resources, derived
// metadata-only s3_bucket_posture facts, bounded s3_external_principal_grant
// facts, and relationships for server-access-log delivery targets. The posture
// fact carries block-public-access flags, default-encryption detail,
// versioning and MFA-delete state, object-ownership / ACL-disabled state,
// access-logging target, replication presence, and booleans derived from the
// bucket policy document. External-principal grant facts carry only public,
// cross-account, AWS service, or unsupported-principal metadata. Object
// inventory, the raw bucket policy JSON, statement bodies, actions, resources,
// conditions, ACL grants, replication rule detail, lifecycle rules,
// notification configuration, and mutation APIs stay outside this package
// contract.
package s3
