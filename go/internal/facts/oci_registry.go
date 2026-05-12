package facts

import "slices"

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
