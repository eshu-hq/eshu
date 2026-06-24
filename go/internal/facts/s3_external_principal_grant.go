// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "slices"

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
