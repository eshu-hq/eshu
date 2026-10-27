// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloud

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/facts/contract"
)

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

// S3BucketPostureFamilyDeclaration is the typed fact-family contract declaration for the
// s3_bucket_posture family (issue #6629). It is build-time metadata: the
// fact-kind-registry generator discovers it statically and derives
// the registry, shards, and catalog from it.
var S3BucketPostureFamilyDeclaration = contract.Declare(contract.Family{
	ID:                     "s3_bucket_posture",
	FactDir:                "go/internal/facts/cloud",
	OwnerRepository:        "eshu-hq/eshu",
	LifecycleOwner:         "go/internal/facts",
	SchemaVersion:          "1.0.0",
	ReducerDomain:          "s3_internet_exposure_materialization",
	ProjectionHook:         "s3_bucket_posture",
	AdmissionHook:          "facts.ValidateSchemaVersion",
	ReadSurface:            "GET /api/v0/cloud/resources",
	TruthProfile:           "provider_gated",
	ProviderKeyIndependent: false,
	PayloadSchema:          "sdk/go/factschema/schema/s3_bucket_posture.v1.schema.json",
	Kinds: []contract.Kind{
		{Kind: "s3_bucket_posture"},
	},
	Producers: []contract.Producer{
		{ID: "collector-aws-cloud", Repository: "eshu-hq/eshu-collector-aws-cloud", Path: ".", PlannedRepository: "", AuthorityMode: "core_issued_grant", Kinds: []string{"s3_bucket_posture"}},
	},
})
