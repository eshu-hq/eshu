// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloud

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/facts/contract"
)

const (
	// SSMHostApplicationFactKind identifies one allowlisted AWS:Application
	// inventory row for one EC2 managed node in one dated capture
	// (eshu-hq/eshu#9556, accepted record #9302). It is an observation, not
	// a package finding: it carries the host tuple, the source envelope and
	// capture time, and the six allowlisted row fields when present — never
	// a vulnerability verdict, supply-chain impact, PURL, or image digest.
	// The satellite collector and the reducer/read surfaces land in sibling
	// #9295 leaves; until they do, observations of this kind produce zero
	// canonical rows.
	SSMHostApplicationFactKind = "aws_ssm_host_application"

	// SSMHostPlatformFactKind identifies the platform observation for one
	// EC2 managed node (eshu-hq/eshu#9556, accepted record #9302): the host
	// tuple, the platform fields when present, the source type with its
	// provenance, and the observation or capture time. A complete newer
	// generation updates the platform observation; a
	// DescribeInstanceInformation observation time must not be labeled
	// inventory capture time.
	SSMHostPlatformFactKind = "aws_ssm_host_platform"

	// SSMHostSchemaVersionV1 is the first SSM host inventory fact schema.
	SSMHostSchemaVersionV1 = "1.0.0"
)

var ssmHostFactKinds = []string{
	SSMHostApplicationFactKind,
	SSMHostPlatformFactKind,
}

var ssmHostSchemaVersions = map[string]string{
	SSMHostApplicationFactKind: SSMHostSchemaVersionV1,
	SSMHostPlatformFactKind:    SSMHostSchemaVersionV1,
}

// SSMHostFactKinds returns the accepted SSM host inventory fact kinds in
// source-contract order. The returned slice is a copy; mutating it does not
// change the registry.
func SSMHostFactKinds() []string {
	return slices.Clone(ssmHostFactKinds)
}

// SSMHostSchemaVersion returns the schema version for an SSM host inventory
// fact kind, and reports whether the kind is registered.
func SSMHostSchemaVersion(factKind string) (string, bool) {
	version, ok := ssmHostSchemaVersions[factKind]
	return version, ok
}

// SSMHostFamilyDeclaration is the typed fact-family contract declaration for
// the aws_ssm_host family (issues #9302 and #9556). It is build-time
// metadata: the fact-kind-registry generator discovers it statically and
// derives the registry, shards, and catalog from it.
//
// Both kinds are consumed by verified reducer bindings (issue #9558): the
// generator rejects a kind that carries both a binding and a disposition, so
// no disposition rides here. The read surface is the SSM host inventory
// readback; the projection hook names the materialization domain that writes
// the served observation nodes.
var SSMHostFamilyDeclaration = contract.Declare(contract.Family{
	ID:                     "aws_ssm_host",
	FactDir:                "go/internal/facts/cloud",
	OwnerRepository:        "eshu-hq/eshu",
	LifecycleOwner:         "go/internal/facts",
	SchemaVersion:          "1.0.0",
	ReducerDomain:          "ssm_host_inventory_materialization",
	ReducerDir:             "go/internal/reducer/ssmhost",
	ProjectionHook:         "ssm_host_inventory_materialization",
	AdmissionHook:          "facts.ValidateSchemaVersion",
	ReadSurface:            "GET /api/v0/cloud/ssm-host-inventory",
	TruthProfile:           "provider_gated",
	ProviderKeyIndependent: false,
	Kinds: []contract.Kind{
		{
			Kind:          "aws_ssm_host_application",
			PayloadSchema: "sdk/go/factschema/schema/aws_ssm_host_application.v1.schema.json",
			ReadSurface:   "GET /api/v0/cloud/ssm-host-inventory",
		},
		{
			Kind:          "aws_ssm_host_platform",
			PayloadSchema: "sdk/go/factschema/schema/aws_ssm_host_platform.v1.schema.json",
			ReadSurface:   "GET /api/v0/cloud/ssm-host-inventory",
		},
	},
	Producers: []contract.Producer{
		{ID: "collector-aws-cloud", Repository: "eshu-hq/eshu-collector-aws-cloud", Path: ".", PlannedRepository: "", AuthorityMode: contract.AuthorityCoreIssuedGrant, Kinds: []string{"aws_ssm_host_application", "aws_ssm_host_platform"}},
	},
})
