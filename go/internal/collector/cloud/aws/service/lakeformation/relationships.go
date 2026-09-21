// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lakeformation

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// resourceS3BucketRelationship derives the registered location's S3 bucket edge
// from the registered location ARN. The bucket ARN inherits the registered
// ARN's partition so the edge joins the bucket node the S3 scanner publishes
// (`arn:<partition>:s3:::<bucket>`). It returns nil when the registered ARN is
// not an S3 location ARN.
func resourceS3BucketRelationship(boundary aws.Boundary, resource RegisteredResource) *aws.RelationshipObservation {
	resourceARN := strings.TrimSpace(resource.ResourceARN)
	bucketARN, bucket, prefix, ok := bucketARNFromS3LocationARN(boundary, resourceARN)
	if !ok {
		return nil
	}
	attributes := map[string]any{
		"registered_location_arn": resourceARN,
		"bucket":                  bucket,
	}
	if prefix != "" {
		attributes["object_key_prefix"] = prefix
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipLakeFormationResourceAtS3Bucket,
		SourceResourceID: resourceARN,
		SourceARN:        resourceARN,
		TargetResourceID: bucketARN,
		TargetARN:        bucketARN,
		TargetType:       aws.ResourceTypeS3Bucket,
		Attributes:       attributes,
		SourceRecordID:   resourceARN + "->" + aws.RelationshipLakeFormationResourceAtS3Bucket + ":" + bucketARN,
	}
}

// resourceRoleRelationship records the IAM role that registered the location.
// It is emitted only when AWS reports a parseable role ARN.
func resourceRoleRelationship(boundary aws.Boundary, resource RegisteredResource) *aws.RelationshipObservation {
	roleARN := strings.TrimSpace(resource.RoleARN)
	if !isARN(roleARN) {
		return nil
	}
	resourceARN := strings.TrimSpace(resource.ResourceARN)
	if resourceARN == "" {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipLakeFormationResourceUsesIAMRole,
		SourceResourceID: resourceARN,
		SourceARN:        resourceARN,
		TargetResourceID: roleARN,
		TargetARN:        roleARN,
		TargetType:       aws.ResourceTypeIAMRole,
		SourceRecordID:   resourceARN + "->" + aws.RelationshipLakeFormationResourceUsesIAMRole + ":" + roleARN,
	}
}

// permissionRelationships returns every graph edge the grant produces: a
// resource edge to the governed Glue database or table, and a principal edge to
// the granted IAM role when the principal identifier is a role ARN. Each edge
// carries a non-empty declared target_type so it never dangles.
func permissionRelationships(boundary aws.Boundary, permissionID string, permission Permission) []aws.RelationshipObservation {
	observations := make([]aws.RelationshipObservation, 0, 2)
	if rel := permissionResourceRelationship(boundary, permissionID, permission); rel != nil {
		observations = append(observations, *rel)
	}
	if rel := permissionPrincipalRelationship(boundary, permissionID, permission); rel != nil {
		observations = append(observations, *rel)
	}
	if len(observations) == 0 {
		return nil
	}
	return observations
}

// permissionResourceRelationship routes the grant to its governed Glue catalog
// node. A table grant (named table or table-wildcard) targets the Glue table;
// a database grant targets the Glue database. Grants on non-catalog resources
// (data locations, LF-Tags, the catalog root) produce no catalog edge here; the
// data-location ARN is already covered by the registered-resource S3 edge.
func permissionResourceRelationship(boundary aws.Boundary, permissionID string, permission Permission) *aws.RelationshipObservation {
	databaseName := strings.TrimSpace(permission.DatabaseName)
	switch strings.TrimSpace(permission.ResourceKind) {
	case "table":
		if permission.TableWildcard {
			// A table-wildcard grant covers every table in the database; there is
			// no single Glue table node to join, so route it to the Glue database
			// node (keyed by the bare database name). Emitting an aws_glue_table
			// edge here would carry a database-shaped id (the Glue table
			// resource_id is "database/table") and never join a table node.
			if databaseName == "" {
				return nil
			}
			return &aws.RelationshipObservation{
				Boundary:         boundary,
				RelationshipType: aws.RelationshipLakeFormationPermissionOnGlueDatabase,
				SourceResourceID: permissionID,
				TargetResourceID: databaseName,
				TargetType:       aws.ResourceTypeGlueDatabase,
				Attributes:       map[string]any{"table_wildcard": true},
				SourceRecordID:   permissionID + "->" + aws.RelationshipLakeFormationPermissionOnGlueDatabase + ":" + databaseName,
			}
		}
		tableID := glueTableResourceID(databaseName, permission.TableName)
		if tableID == "" {
			return nil
		}
		return &aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipLakeFormationPermissionOnGlueTable,
			SourceResourceID: permissionID,
			TargetResourceID: tableID,
			TargetType:       aws.ResourceTypeGlueTable,
			Attributes:       map[string]any{"database_name": databaseName},
			SourceRecordID:   permissionID + "->" + aws.RelationshipLakeFormationPermissionOnGlueTable + ":" + tableID,
		}
	case "database":
		if databaseName == "" {
			return nil
		}
		return &aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipLakeFormationPermissionOnGlueDatabase,
			SourceResourceID: permissionID,
			TargetResourceID: databaseName,
			TargetType:       aws.ResourceTypeGlueDatabase,
			SourceRecordID:   permissionID + "->" + aws.RelationshipLakeFormationPermissionOnGlueDatabase + ":" + databaseName,
		}
	default:
		return nil
	}
}

// permissionPrincipalRelationship records the IAM role a grant is granted to.
// It is emitted only when the principal identifier is an IAM role ARN, so a
// special principal (IAM_ALLOWED_PRINCIPALS) or a non-role ARN does not produce
// a dangling or mistyped edge.
func permissionPrincipalRelationship(boundary aws.Boundary, permissionID string, permission Permission) *aws.RelationshipObservation {
	principalID := strings.TrimSpace(permission.PrincipalID)
	if !isRoleARN(principalID) {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipLakeFormationPermissionGrantedToPrincipal,
		SourceResourceID: permissionID,
		TargetResourceID: principalID,
		TargetARN:        principalID,
		TargetType:       aws.ResourceTypeIAMRole,
		SourceRecordID:   permissionID + "->" + aws.RelationshipLakeFormationPermissionGrantedToPrincipal + ":" + principalID,
	}
}
