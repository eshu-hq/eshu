// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloud

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/facts/contract"
)

const (
	// RDSInstancePostureFactKind identifies one derived security/operations
	// posture observation for an RDS DB instance or Aurora DB cluster. It is
	// metadata-only control-plane evidence: derived booleans, retention
	// windows, and KMS/parameter/option-group identifiers reported by the RDS
	// describe APIs. It never carries database contents, master usernames,
	// connection secrets, snapshot payloads, log bodies, or Performance
	// Insights samples. The fact is source evidence only; reducers own any
	// graph edges, internet-exposure derivation, or posture truth promotion.
	RDSInstancePostureFactKind = "rds_instance_posture"

	// RDSPostureSchemaVersionV1 is the first RDS posture fact schema.
	RDSPostureSchemaVersionV1 = "1.0.0"
)

var rdsPostureFactKinds = []string{
	RDSInstancePostureFactKind,
}

var rdsPostureSchemaVersions = map[string]string{
	RDSInstancePostureFactKind: RDSPostureSchemaVersionV1,
}

// RDSPostureFactKinds returns the accepted RDS posture fact kinds in their
// source-contract order. The returned slice is a copy; callers may mutate it
// without affecting the registry.
func RDSPostureFactKinds() []string {
	return slices.Clone(rdsPostureFactKinds)
}

// RDSPostureSchemaVersion returns the schema version for an RDS posture fact
// kind and reports whether the kind is registered.
func RDSPostureSchemaVersion(factKind string) (string, bool) {
	version, ok := rdsPostureSchemaVersions[factKind]
	return version, ok
}

// RDSPostureFamilyDeclaration is the typed fact-family contract declaration for the
// rds_posture family (issue #6629). It is build-time metadata: the
// fact-kind-registry generator discovers it statically and derives
// the registry, shards, and catalog from it.
var RDSPostureFamilyDeclaration = contract.Declare(contract.Family{
	ID:                     "rds_posture",
	FactDir:                "go/internal/facts/cloud",
	OwnerRepository:        "eshu-hq/eshu",
	LifecycleOwner:         "go/internal/facts",
	SchemaVersion:          "1.0.0",
	ReducerDomain:          "rds_posture_materialization",
	ProjectionHook:         "rds_posture_materialization",
	AdmissionHook:          "facts.ValidateSchemaVersion",
	ReadSurface:            "GET /api/v0/cloud/resources",
	TruthProfile:           "provider_gated",
	ProviderKeyIndependent: false,
	PayloadSchema:          "sdk/go/factschema/schema/rds_instance_posture.v1.schema.json",
	Kinds: []contract.Kind{
		{Kind: "rds_instance_posture"},
	},
	Producers: []contract.Producer{
		{ID: "collector-aws-cloud", Repository: "eshu-hq/eshu-collector-aws-cloud", Path: ".", PlannedRepository: "", AuthorityMode: "core_issued_grant", Kinds: []string{"rds_instance_posture"}},
	},
})
