package facts

import "slices"

const (
	// GCPCloudResourceFactKind identifies one Cloud Asset Inventory resource
	// observation reported by the GCP cloud collector. It is the provider-specific
	// source fact for GCP resource inventory; reducers admit it into the shared
	// CloudResource keyspace. It is deliberately not a generic cloud_resource fact.
	GCPCloudResourceFactKind = "gcp_cloud_resource"
	// GCPCollectionWarningFactKind identifies one explicit partial, unsupported,
	// stale, permission-hidden, quota, or redaction outcome from the GCP cloud
	// collector. It is the GCP analog of aws_warning and records coverage gaps as
	// durable evidence rather than letting them degrade silently into success.
	GCPCollectionWarningFactKind = "gcp_collection_warning"

	// GCPCloudResourceSchemaVersion is the first GCP cloud resource fact schema.
	GCPCloudResourceSchemaVersion = "1.0.0"
	// GCPCollectionWarningSchemaVersion is the first GCP collection warning fact
	// schema.
	GCPCollectionWarningSchemaVersion = "1.0.0"
)

var gcpFactKinds = []string{
	GCPCloudResourceFactKind,
	GCPCollectionWarningFactKind,
}

var gcpSchemaVersions = map[string]string{
	GCPCloudResourceFactKind:     GCPCloudResourceSchemaVersion,
	GCPCollectionWarningFactKind: GCPCollectionWarningSchemaVersion,
}

// GCPFactKinds returns the accepted GCP fact kinds in their emission order.
func GCPFactKinds() []string {
	return slices.Clone(gcpFactKinds)
}

// GCPSchemaVersion returns the schema version for a GCP fact kind.
func GCPSchemaVersion(factKind string) (string, bool) {
	version, ok := gcpSchemaVersions[factKind]
	return version, ok
}
