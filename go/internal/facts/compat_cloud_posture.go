// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

// This file is the facts root's transitional compatibility surface for the
// derived cloud-posture fact families, which moved to [cloud] in issue #6776
// so the root directory drops back under the 40-file dirgate cap. Every
// entry is an alias or a thin forwarder with no behavior change: the value,
// the type identity, and the returned bytes are the same ones the root
// declared before the move.
//
// It carries only the names that still have a caller -- the facts root's own
// schemaVersionFamilies registry wiring plus the collectors, reducers,
// projectors, and query surfaces that reach them as facts.X today. A later
// family move adds a stanza to this file and never creates a new
// compat_*.go, and each entry here is deleted once its last caller has moved
// to the cloud package directly (see the importer-migration follow-up,
// #6950).

import "github.com/eshu-hq/eshu/go/internal/facts/cloud"

// Stanza: ec2_instance_posture.go (moved to cloud/ec2_instance_posture.go).
const (
	// EC2InstancePostureFactKind identifies one derived security/operations
	// posture observation for an EC2 instance. It is metadata-only control-plane
	// evidence read from the existing DescribeInstances pass: IMDS settings
	// (whether IMDSv2 is required, the hop limit, and the endpoint state), user-
	// data PRESENCE as a boolean only, detailed-monitoring and EBS-optimized
	// flags, public-IP association, the attached instance-profile ARN, per-volume
	// block-device metadata, and tenancy / Nitro-enclave state. It NEVER carries
	// the user-data content (which can embed secrets), instance console output,
	// environment variables, command-line arguments, or any other instance
	// payload. The fact is source evidence only; reducers own the USES_PROFILE
	// join to the IAM instance profile (#1146), the block-device KMS posture
	// projection (#1304), and the derived internet-exposed flag (#1135). See
	// [cloud.EC2InstancePostureFactKind].
	EC2InstancePostureFactKind = cloud.EC2InstancePostureFactKind
	// EC2InstancePostureSchemaVersionV1 is the first EC2 instance posture fact
	// schema. See [cloud.EC2InstancePostureSchemaVersionV1].
	EC2InstancePostureSchemaVersionV1 = cloud.EC2InstancePostureSchemaVersionV1
)

// EC2InstancePostureFactKinds returns the accepted EC2 instance posture fact
// kinds in source-contract order. The returned slice is a copy; mutating it
// does not change the registry. See [cloud.EC2InstancePostureFactKinds].
func EC2InstancePostureFactKinds() []string {
	return cloud.EC2InstancePostureFactKinds()
}

// EC2InstancePostureSchemaVersion returns the schema version for an EC2
// instance posture fact kind, and reports whether the kind is registered. See
// [cloud.EC2InstancePostureSchemaVersion].
func EC2InstancePostureSchemaVersion(factKind string) (string, bool) {
	return cloud.EC2InstancePostureSchemaVersion(factKind)
}

// Stanza: rds_posture.go (moved to cloud/rds_posture.go).
const (
	// RDSInstancePostureFactKind identifies one derived security/operations
	// posture observation for an RDS DB instance or Aurora DB cluster. It is
	// metadata-only control-plane evidence: derived booleans, retention windows,
	// and KMS/parameter/option-group identifiers reported by the RDS describe
	// APIs. It never carries database contents, master usernames, connection
	// secrets, snapshot payloads, log bodies, or Performance Insights samples.
	// The fact is source evidence only; reducers own any graph edges, internet-
	// exposure derivation, or posture truth promotion. See
	// [cloud.RDSInstancePostureFactKind].
	RDSInstancePostureFactKind = cloud.RDSInstancePostureFactKind
	// RDSPostureSchemaVersionV1 is the first RDS posture fact schema. See
	// [cloud.RDSPostureSchemaVersionV1].
	RDSPostureSchemaVersionV1 = cloud.RDSPostureSchemaVersionV1
)

// RDSPostureFactKinds returns the accepted RDS posture fact kinds in their
// source-contract order. The returned slice is a copy; callers may mutate it
// without affecting the registry. See [cloud.RDSPostureFactKinds].
func RDSPostureFactKinds() []string {
	return cloud.RDSPostureFactKinds()
}

// RDSPostureSchemaVersion returns the schema version for an RDS posture fact
// kind and reports whether the kind is registered. See
// [cloud.RDSPostureSchemaVersion].
func RDSPostureSchemaVersion(factKind string) (string, bool) {
	return cloud.RDSPostureSchemaVersion(factKind)
}

// Stanza: s3_bucket_posture.go (moved to cloud/s3_bucket_posture.go).
const (
	// S3BucketPostureFactKind identifies one derived S3 bucket security-posture
	// fact. It is metadata-only posture evidence reported by the AWS collector:
	// block-public-access flags, default-encryption detail (SSE-KMS key ARN and
	// bucket-key state), versioning and MFA-delete state, object-ownership / ACL-
	// disabled state, access-logging target, replication presence, and booleans
	// DERIVED from the bucket policy document (public grant, cross-account
	// principal). It never carries the raw bucket policy JSON, ACL grants, or
	// object data. It is source evidence only; reducer graph projection of this
	// posture is a separate consumer. See [cloud.S3BucketPostureFactKind].
	S3BucketPostureFactKind = cloud.S3BucketPostureFactKind
	// S3BucketPostureSchemaVersionV1 is the first S3 bucket posture fact schema.
	// See [cloud.S3BucketPostureSchemaVersionV1].
	S3BucketPostureSchemaVersionV1 = cloud.S3BucketPostureSchemaVersionV1
)

// S3BucketPostureFactKinds returns the accepted S3 bucket posture fact kinds
// in source-contract order. The returned slice is a copy; mutating it does not
// change the registry. See [cloud.S3BucketPostureFactKinds].
func S3BucketPostureFactKinds() []string {
	return cloud.S3BucketPostureFactKinds()
}

// S3BucketPostureSchemaVersion returns the schema version for an S3 bucket
// posture fact kind, and reports whether the kind is registered. See
// [cloud.S3BucketPostureSchemaVersion].
func S3BucketPostureSchemaVersion(factKind string) (string, bool) {
	return cloud.S3BucketPostureSchemaVersion(factKind)
}

// Stanza: s3_external_principal_grant.go (moved to cloud/s3_external_principal_grant.go).
const (
	// S3ExternalPrincipalGrantFactKind identifies one metadata-only S3 bucket
	// policy grant to an external principal. It is reported AWS collector
	// evidence derived from a transient bucket-policy parse and never carries raw
	// policy JSON, statements, actions, resources, condition values, ACL grants,
	// object keys, or object data. Reducers own any later ExternalPrincipal graph
	// projection. See [cloud.S3ExternalPrincipalGrantFactKind].
	S3ExternalPrincipalGrantFactKind = cloud.S3ExternalPrincipalGrantFactKind
	// S3ExternalPrincipalGrantSchemaVersionV1 is the first S3 external-principal
	// grant fact schema. See [cloud.S3ExternalPrincipalGrantSchemaVersionV1].
	S3ExternalPrincipalGrantSchemaVersionV1 = cloud.S3ExternalPrincipalGrantSchemaVersionV1
)

// S3ExternalPrincipalGrantFactKinds returns the accepted S3 external-principal
// grant fact kinds in source-contract order. The returned slice is a copy;
// mutating it does not change the registry. See
// [cloud.S3ExternalPrincipalGrantFactKinds].
func S3ExternalPrincipalGrantFactKinds() []string {
	return cloud.S3ExternalPrincipalGrantFactKinds()
}

// S3ExternalPrincipalGrantSchemaVersion returns the schema version for an S3
// external-principal grant fact kind, and reports whether the kind is
// registered. See [cloud.S3ExternalPrincipalGrantSchemaVersion].
func S3ExternalPrincipalGrantSchemaVersion(factKind string) (string, bool) {
	return cloud.S3ExternalPrincipalGrantSchemaVersion(factKind)
}
