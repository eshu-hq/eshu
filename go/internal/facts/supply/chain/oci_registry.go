// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/facts/contract"
)

const (
	// OCIRegistryRepositoryFactKind identifies one OCI registry repository
	// configured for observation.
	OCIRegistryRepositoryFactKind = "oci_registry.repository"
	// OCIImageTagObservationFactKind identifies one mutable tag-to-digest
	// observation.
	OCIImageTagObservationFactKind = "oci_registry.image_tag_observation"
	// OCIImageManifestFactKind identifies one digest-addressed image manifest.
	OCIImageManifestFactKind = "oci_registry.image_manifest"
	// OCIImageIndexFactKind identifies one digest-addressed image index.
	OCIImageIndexFactKind = "oci_registry.image_index"
	// OCIImageDescriptorFactKind identifies one reusable OCI descriptor.
	OCIImageDescriptorFactKind = "oci_registry.image_descriptor"
	// OCIImageReferrerFactKind identifies one descriptor reported as a referrer.
	OCIImageReferrerFactKind = "oci_registry.image_referrer"
	// OCIRegistryWarningFactKind identifies one non-fatal OCI registry warning.
	OCIRegistryWarningFactKind = "oci_registry.warning"

	// OCIRegistryRepositorySchemaVersion is the first repository fact schema.
	OCIRegistryRepositorySchemaVersion = "1.0.0"
	// OCIImageTagObservationSchemaVersion is the first tag fact schema.
	OCIImageTagObservationSchemaVersion = "1.0.0"
	// OCIImageManifestSchemaVersion is the first manifest fact schema.
	OCIImageManifestSchemaVersion = "1.0.0"
	// OCIImageIndexSchemaVersion is the first image-index fact schema.
	OCIImageIndexSchemaVersion = "1.0.0"
	// OCIImageDescriptorSchemaVersion is the first descriptor fact schema.
	OCIImageDescriptorSchemaVersion = "1.0.0"
	// OCIImageReferrerSchemaVersion is the first referrer fact schema.
	OCIImageReferrerSchemaVersion = "1.0.0"
	// OCIRegistryWarningSchemaVersion is the first warning fact schema.
	OCIRegistryWarningSchemaVersion = "1.0.0"
)

var ociRegistryFactKinds = []string{
	OCIRegistryRepositoryFactKind,
	OCIImageTagObservationFactKind,
	OCIImageManifestFactKind,
	OCIImageIndexFactKind,
	OCIImageDescriptorFactKind,
	OCIImageReferrerFactKind,
	OCIRegistryWarningFactKind,
}

var ociRegistrySchemaVersions = map[string]string{
	OCIRegistryRepositoryFactKind:  OCIRegistryRepositorySchemaVersion,
	OCIImageTagObservationFactKind: OCIImageTagObservationSchemaVersion,
	OCIImageManifestFactKind:       OCIImageManifestSchemaVersion,
	OCIImageIndexFactKind:          OCIImageIndexSchemaVersion,
	OCIImageDescriptorFactKind:     OCIImageDescriptorSchemaVersion,
	OCIImageReferrerFactKind:       OCIImageReferrerSchemaVersion,
	OCIRegistryWarningFactKind:     OCIRegistryWarningSchemaVersion,
}

// OCIRegistryFactKinds returns the accepted OCI registry fact kinds in their
// emission order.
func OCIRegistryFactKinds() []string {
	return slices.Clone(ociRegistryFactKinds)
}

// OCIRegistrySchemaVersion returns the schema version for an OCI registry fact
// kind.
func OCIRegistrySchemaVersion(factKind string) (string, bool) {
	version, ok := ociRegistrySchemaVersions[factKind]
	return version, ok
}

// OCIRegistryFamilyDeclaration is the typed fact-family contract declaration for the
// oci_registry family (issue #6629). It is build-time metadata: the
// fact-kind-registry generator discovers it statically and derives
// the registry, shards, and catalog from it.
var OCIRegistryFamilyDeclaration = contract.Declare(contract.Family{
	ID:                     "oci_registry",
	FactDir:                "go/internal/facts/supply/chain",
	OwnerRepository:        "eshu-hq/eshu",
	LifecycleOwner:         "go/internal/facts",
	SchemaVersion:          "1.0.0",
	ReducerDomain:          "container_image_identity",
	ProjectionHook:         "container_image_identity",
	AdmissionHook:          "facts.ValidateSchemaVersion",
	ReadSurface:            "GET /api/v0/images",
	TruthProfile:           "provider_gated",
	ProviderKeyIndependent: false,
	Kinds: []contract.Kind{
		{Kind: "oci_registry.image_descriptor", PayloadSchema: "sdk/go/factschema/schema/oci_registry.image_descriptor.v1.schema.json"},
		{Kind: "oci_registry.image_index", PayloadSchema: "sdk/go/factschema/schema/oci_registry.image_index.v1.schema.json"},
		{Kind: "oci_registry.image_manifest", PayloadSchema: "sdk/go/factschema/schema/oci_registry.image_manifest.v1.schema.json"},
		{Kind: "oci_registry.image_referrer", PayloadSchema: "sdk/go/factschema/schema/oci_registry.image_referrer.v1.schema.json"},
		{Kind: "oci_registry.image_tag_observation", PayloadSchema: "sdk/go/factschema/schema/oci_registry.image_tag_observation.v1.schema.json"},
		{Kind: "oci_registry.repository", PayloadSchema: "sdk/go/factschema/schema/oci_registry.repository.v1.schema.json", Disposition: "projector_only", NoReducerReason: "Consumed by canonical projection; no reducer domain decodes it.", NoReducerEvidence: "projector consumption in go/internal/projector/canonical, go/internal/projector/decode"},
		{Kind: "oci_registry.warning", PayloadSchema: "sdk/go/factschema/schema/oci_registry.warning.v1.schema.json"},
	},
	Producers: []contract.Producer{
		{ID: "collector-oci-registry", Repository: "eshu-hq/eshu", Path: "go/internal/collector/ociregistry", PlannedRepository: "eshu-hq/eshu-collector-oci-registry", AuthorityMode: "core_issued_grant", Kinds: []string{"oci_registry.image_descriptor", "oci_registry.image_index", "oci_registry.image_manifest", "oci_registry.image_referrer", "oci_registry.image_tag_observation", "oci_registry.repository", "oci_registry.warning"}},
	},
})
