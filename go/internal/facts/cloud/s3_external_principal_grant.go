// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloud

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/facts/contract"
)

const (
	// S3ExternalPrincipalGrantFactKind identifies one metadata-only S3 bucket
	// policy grant to an external principal. It is reported AWS collector
	// evidence derived from a transient bucket-policy parse and never carries raw
	// policy JSON, statements, actions, resources, condition values, ACL grants,
	// object keys, or object data. Reducers own any later ExternalPrincipal graph
	// projection.
	S3ExternalPrincipalGrantFactKind = "s3_external_principal_grant"

	// S3ExternalPrincipalGrantSchemaVersionV1 is the first S3 external-principal
	// grant fact schema.
	S3ExternalPrincipalGrantSchemaVersionV1 = "1.0.0"
)

var s3ExternalPrincipalGrantFactKinds = []string{
	S3ExternalPrincipalGrantFactKind,
}

var s3ExternalPrincipalGrantSchemaVersions = map[string]string{
	S3ExternalPrincipalGrantFactKind: S3ExternalPrincipalGrantSchemaVersionV1,
}

// S3ExternalPrincipalGrantFactKinds returns the accepted S3 external-principal
// grant fact kinds in source-contract order. The returned slice is a copy;
// mutating it does not change the registry.
func S3ExternalPrincipalGrantFactKinds() []string {
	return slices.Clone(s3ExternalPrincipalGrantFactKinds)
}

// S3ExternalPrincipalGrantSchemaVersion returns the schema version for an S3
// external-principal grant fact kind, and reports whether the kind is
// registered.
func S3ExternalPrincipalGrantSchemaVersion(factKind string) (string, bool) {
	version, ok := s3ExternalPrincipalGrantSchemaVersions[factKind]
	return version, ok
}

// S3ExternalPrincipalGrantFamilyDeclaration is the typed fact-family contract declaration for the
// s3_external_principal_grant family (issue #6629). It is build-time metadata: the
// fact-kind-registry generator discovers it statically and derives
// the registry, shards, and catalog from it.
var S3ExternalPrincipalGrantFamilyDeclaration = contract.Declare(contract.Family{
	ID:                     "s3_external_principal_grant",
	FactDir:                "go/internal/facts/cloud",
	OwnerRepository:        "eshu-hq/eshu",
	LifecycleOwner:         "go/internal/facts",
	SchemaVersion:          "1.0.0",
	ReducerDomain:          "s3_external_principal_grant_materialization",
	ProjectionHook:         "s3_external_principal_grant_materialization",
	AdmissionHook:          "facts.ValidateSchemaVersion",
	ReadSurface:            "GET /api/v0/secrets-iam/posture-summary",
	TruthProfile:           "provider_gated",
	ProviderKeyIndependent: false,
	PayloadSchema:          "sdk/go/factschema/schema/s3_external_principal_grant.v1.schema.json",
	Kinds: []contract.Kind{
		{Kind: "s3_external_principal_grant"},
	},
	Producers: []contract.Producer{
		{ID: "collector-aws-cloud", Repository: "eshu-hq/eshu", Path: "go/internal/collector/awscloud", PlannedRepository: "eshu-hq/eshu-collector-aws-cloud", AuthorityMode: "core_issued_grant", Kinds: []string{"s3_external_principal_grant"}},
	},
})
