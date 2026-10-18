// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimage

import "github.com/eshu-hq/eshu/go/internal/facts/contract"

// AWSHandleBinding declares this package's verified reducer consumption of
// aws kinds (issue #6629). Build-time metadata only.
var AWSHandleBinding = contract.Bind(contract.Binding{
	ID: "containerimage/containerimage_handle_aws", FamilyID: "aws",
	ReducerDir: "go/internal/reducer/containerimage", Symbol: "Handle",
	Kinds: []string{"aws_image_reference", "aws_relationship"},
})

// AzureHandleBinding declares this package's verified reducer consumption of
// azure kinds (issue #6629). Build-time metadata only.
var AzureHandleBinding = contract.Bind(contract.Binding{
	ID: "containerimage/containerimage_handle_azure", FamilyID: "azure",
	ReducerDir: "go/internal/reducer/containerimage", Symbol: "Handle",
	Kinds: []string{"azure_image_reference"},
})

// CICDRunHandleBinding declares this package's verified reducer consumption of
// ci_cd_run kinds (issue #6629). Build-time metadata only.
var CICDRunHandleBinding = contract.Bind(contract.Binding{
	ID: "containerimage/containerimage_handle_ci_cd_run", FamilyID: "ci_cd_run",
	ReducerDir: "go/internal/reducer/containerimage", Symbol: "Handle",
	Kinds: []string{"ci.artifact", "ci.run", "ci.workflow_image_evidence"},
})

// GCPHandleBinding declares this package's verified reducer consumption of
// gcp kinds (issue #6629). Build-time metadata only.
var GCPHandleBinding = contract.Bind(contract.Binding{
	ID: "containerimage/containerimage_handle_gcp", FamilyID: "gcp",
	ReducerDir: "go/internal/reducer/containerimage", Symbol: "Handle",
	Kinds: []string{"gcp_image_reference"},
})

// OCIRegistryHandleBinding declares this package's verified reducer consumption of
// oci_registry kinds (issue #6629). Build-time metadata only.
var OCIRegistryHandleBinding = contract.Bind(contract.Binding{
	ID: "containerimage/containerimage_handle_oci_registry", FamilyID: "oci_registry",
	ReducerDir: "go/internal/reducer/containerimage", Symbol: "Handle",
	Kinds: []string{"oci_registry.image_index", "oci_registry.image_manifest", "oci_registry.image_tag_observation", "oci_registry.warning"},
})

// SBOMAttestationHandleBinding declares this package's verified reducer consumption of
// sbom_attestation kinds (issue #6629). Build-time metadata only.
var SBOMAttestationHandleBinding = contract.Bind(contract.Binding{
	ID: "containerimage/containerimage_handle_sbom_attestation", FamilyID: "sbom_attestation",
	ReducerDir: "go/internal/reducer/containerimage", Symbol: "Handle",
	Kinds: []string{"attestation.signature_verification", "attestation.slsa_provenance", "attestation.statement"},
})
