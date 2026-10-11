// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

// This file is the facts root's transitional compatibility surface for the
// cloud-provider inventory fact families, which moved to [cloud] in issue
// #6776 so the root directory drops back under the 40-file dirgate cap.
// Every entry is an alias or a thin forwarder with no behavior change: the
// value, the type identity, and the returned bytes are the same ones the
// root declared before the move.
//
// It carries only the names that still have a caller -- the facts root's own
// schemaVersionFamilies registry wiring and registry tests plus the
// collectors, reducers, projectors, and query surfaces that reach them as
// facts.X today. A later family move adds a stanza to this file and never
// creates a new compat_*.go, and each entry here is deleted once its last
// caller has moved to the cloud package directly (see the importer-migration
// follow-up, #6950).
//
// Omitted deliberately because nothing references it:
// AWSTagObservationSchemaVersion. Reach it as
// cloud.AWSTagObservationSchemaVersion; AWSSchemaVersion("aws_tag_observation")
// still returns its value.

import "github.com/eshu-hq/eshu/go/internal/facts/cloud"

// Stanza: aws.go (moved to cloud/aws.go).
const (
	// AWSIAMPermissionFactKind identifies one derived IAM permission statement.
	// It is the normalized, metadata-only projection of a single IAM policy
	// statement attached to a principal: effect, action set, resource pattern,
	// and a condition summary. It NEVER carries the raw policy JSON body or any
	// condition values. PR1 emits this fact; the reducer graph projection that
	// consumes it ships separately under principal review (issue #1134). See
	// [cloud.AWSIAMPermissionFactKind].
	AWSIAMPermissionFactKind = cloud.AWSIAMPermissionFactKind
	// AWSIAMPermissionSchemaVersion is the first derived IAM permission schema.
	// See [cloud.AWSIAMPermissionSchemaVersion].
	AWSIAMPermissionSchemaVersion = cloud.AWSIAMPermissionSchemaVersion
	// AWSRelationshipFactKind identifies one relationship reported by AWS APIs.
	// See [cloud.AWSRelationshipFactKind].
	AWSRelationshipFactKind = cloud.AWSRelationshipFactKind
	// AWSRelationshipSchemaVersion is the first AWS relationship fact schema. See
	// [cloud.AWSRelationshipSchemaVersion].
	AWSRelationshipSchemaVersion = cloud.AWSRelationshipSchemaVersion
	// AWSResourceFactKind identifies one resource reported by an AWS API. See
	// [cloud.AWSResourceFactKind].
	AWSResourceFactKind = cloud.AWSResourceFactKind
	// AWSResourceSchemaVersion is the first AWS resource fact schema. See
	// [cloud.AWSResourceSchemaVersion].
	AWSResourceSchemaVersion = cloud.AWSResourceSchemaVersion
	// AWSWarningFactKind identifies one non-fatal AWS scanner warning. See
	// [cloud.AWSWarningFactKind].
	AWSWarningFactKind = cloud.AWSWarningFactKind
	// AWSWarningSchemaVersion is the first AWS warning fact schema. See
	// [cloud.AWSWarningSchemaVersion].
	AWSWarningSchemaVersion = cloud.AWSWarningSchemaVersion
)

// AWSSchemaVersion returns the schema version for an AWS fact kind. See
// [cloud.AWSSchemaVersion].
func AWSSchemaVersion(factKind string) (string, bool) {
	return cloud.AWSSchemaVersion(factKind)
}
