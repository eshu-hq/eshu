// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbomattest

import "github.com/eshu-hq/eshu/go/internal/facts/contract"

// OCIRegistryHandleBinding declares this package's verified reducer consumption of
// oci_registry kinds (issue #6629). Build-time metadata only.
var OCIRegistryHandleBinding = contract.Bind(contract.Binding{
	ID: "sbomattest/sbomattest_handle_oci_registry", FamilyID: "oci_registry",
	ReducerDir: "go/internal/reducer/sbomattest", Symbol: "Handle",
	Kinds: []string{"oci_registry.image_referrer"},
})

// SBOMAttestationHandleBinding declares this package's verified reducer consumption of
// sbom_attestation kinds (issue #6629). Build-time metadata only.
var SBOMAttestationHandleBinding = contract.Bind(contract.Binding{
	ID: "sbomattest/sbomattest_handle_sbom_attestation", FamilyID: "sbom_attestation",
	ReducerDir: "go/internal/reducer/sbomattest", Symbol: "Handle",
	Kinds: []string{"attestation.signature_verification", "attestation.slsa_provenance", "attestation.statement", "sbom.component", "sbom.dependency_relationship", "sbom.document", "sbom.external_reference", "sbom.warning"},
})
