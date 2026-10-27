// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloud

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/facts/contract"
)

const (
	// EC2InstancePostureFactKind identifies one derived security/operations
	// posture observation for an EC2 instance. It is metadata-only control-plane
	// evidence read from the existing DescribeInstances pass: IMDS settings
	// (whether IMDSv2 is required, the hop limit, and the endpoint state),
	// user-data PRESENCE as a boolean only, detailed-monitoring and
	// EBS-optimized flags, public-IP association, the attached instance-profile
	// ARN, per-volume block-device metadata, and tenancy / Nitro-enclave state.
	//
	// It NEVER carries the user-data content (which can embed secrets), instance
	// console output, environment variables, command-line arguments, or any
	// other instance payload. The fact is source evidence only; reducers own the
	// USES_PROFILE join to the IAM instance profile (#1146), the block-device KMS
	// posture projection (#1304), and the derived internet-exposed flag (#1135).
	EC2InstancePostureFactKind = "ec2_instance_posture"

	// EC2InstancePostureSchemaVersionV1 is the first EC2 instance posture fact
	// schema.
	EC2InstancePostureSchemaVersionV1 = "1.0.0"
)

var ec2InstancePostureFactKinds = []string{
	EC2InstancePostureFactKind,
}

var ec2InstancePostureSchemaVersions = map[string]string{
	EC2InstancePostureFactKind: EC2InstancePostureSchemaVersionV1,
}

// EC2InstancePostureFactKinds returns the accepted EC2 instance posture fact
// kinds in source-contract order. The returned slice is a copy; mutating it does
// not change the registry.
func EC2InstancePostureFactKinds() []string {
	return slices.Clone(ec2InstancePostureFactKinds)
}

// EC2InstancePostureSchemaVersion returns the schema version for an EC2 instance
// posture fact kind, and reports whether the kind is registered.
func EC2InstancePostureSchemaVersion(factKind string) (string, bool) {
	version, ok := ec2InstancePostureSchemaVersions[factKind]
	return version, ok
}

// EC2InstancePostureFamilyDeclaration is the typed fact-family contract declaration for the
// ec2_instance_posture family (issue #6629). It is build-time metadata: the
// fact-kind-registry generator discovers it statically and derives
// the registry, shards, and catalog from it.
var EC2InstancePostureFamilyDeclaration = contract.Declare(contract.Family{
	ID:                     "ec2_instance_posture",
	FactDir:                "go/internal/facts/cloud",
	OwnerRepository:        "eshu-hq/eshu",
	LifecycleOwner:         "go/internal/facts",
	SchemaVersion:          "1.0.0",
	ReducerDomain:          "ec2_instance_node_materialization",
	ProjectionHook:         "ec2_instance_posture",
	AdmissionHook:          "facts.ValidateSchemaVersion",
	ReadSurface:            "GET /api/v0/cloud/resources",
	TruthProfile:           "provider_gated",
	ProviderKeyIndependent: false,
	Kinds: []contract.Kind{
		{Kind: "ec2_instance_posture"},
	},
	Producers: []contract.Producer{
		{ID: "collector-aws-cloud", Repository: "eshu-hq/eshu-collector-aws-cloud", Path: ".", PlannedRepository: "", AuthorityMode: "core_issued_grant", Kinds: []string{"ec2_instance_posture"}},
	},
})
