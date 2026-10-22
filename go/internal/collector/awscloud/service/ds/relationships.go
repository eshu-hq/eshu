// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ds

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/catalog"
)

// directoryRelationships returns the VPC and subnet edges reported by one
// directory. Each edge sets a non-empty target_type and a target_resource_id
// that matches the target scanner's resource_id: VPC and subnet edges use the
// bare AWS ID (joining the VPC scanner's aws_ec2_vpc and aws_ec2_subnet
// resources).
func directoryRelationships(boundary awscloud.Boundary, directory Directory) []awscloud.RelationshipObservation {
	sourceID := strings.TrimSpace(directory.ID)
	if sourceID == "" {
		return nil
	}
	var relationships []awscloud.RelationshipObservation

	if vpcID := strings.TrimSpace(directory.VPCID); vpcID != "" {
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: catalog.RelationshipDSDirectoryInVPC,
			SourceResourceID: sourceID,
			TargetResourceID: vpcID,
			TargetType:       catalog.ResourceTypeEC2VPC,
			Attributes:       map[string]any{"vpc_id": vpcID},
			SourceRecordID:   relationshipRecordID(sourceID, catalog.RelationshipDSDirectoryInVPC, vpcID),
		})
	}
	for _, subnetID := range cloneStrings(directory.SubnetIDs) {
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: catalog.RelationshipDSDirectoryInSubnet,
			SourceResourceID: sourceID,
			TargetResourceID: subnetID,
			TargetType:       catalog.ResourceTypeEC2Subnet,
			Attributes:       map[string]any{"subnet_id": subnetID},
			SourceRecordID:   relationshipRecordID(sourceID, catalog.RelationshipDSDirectoryInSubnet, subnetID),
		})
	}
	return relationships
}

// trustRelationships returns the trust-to-directory edge for one trust. The
// target is the bare directory id so it joins the directory resource fact emitted
// in the same scan.
func trustRelationships(boundary awscloud.Boundary, trust Trust) []awscloud.RelationshipObservation {
	sourceID := strings.TrimSpace(trust.ID)
	directoryID := strings.TrimSpace(trust.DirectoryID)
	if sourceID == "" || directoryID == "" {
		return nil
	}
	return []awscloud.RelationshipObservation{{
		Boundary:         boundary,
		RelationshipType: catalog.RelationshipDSTrustTargetsDirectory,
		SourceResourceID: sourceID,
		TargetResourceID: directoryID,
		TargetType:       catalog.ResourceTypeDSDirectory,
		Attributes:       map[string]any{"directory_id": directoryID},
		SourceRecordID:   relationshipRecordID(sourceID, catalog.RelationshipDSTrustTargetsDirectory, directoryID),
	}}
}

// sharedDirectoryRelationships returns the owner-directory and owner-account
// edges for one shared-directory invitation. The owner-directory edge targets the
// bare owner directory id (joining the directory resource fact when in scope),
// and the owner-account edge targets the bare 12-digit account id; no ARN is
// synthesized for either target.
func sharedDirectoryRelationships(
	boundary awscloud.Boundary,
	share SharedDirectory,
	directoryIDs map[string]struct{},
) []awscloud.RelationshipObservation {
	sourceID := sharedDirectoryResourceID(share)
	if sourceID == "" {
		return nil
	}
	var relationships []awscloud.RelationshipObservation

	if ownerDirectoryID := strings.TrimSpace(share.OwnerDirectoryID); ownerDirectoryID != "" {
		_, inScope := directoryIDs[ownerDirectoryID]
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: catalog.RelationshipDSSharedDirectoryTargetsOwnerDirectory,
			SourceResourceID: sourceID,
			TargetResourceID: ownerDirectoryID,
			TargetType:       catalog.ResourceTypeDSDirectory,
			Attributes: map[string]any{
				"owner_directory_id": ownerDirectoryID,
				"in_scope":           inScope,
			},
			SourceRecordID: relationshipRecordID(sourceID, catalog.RelationshipDSSharedDirectoryTargetsOwnerDirectory, ownerDirectoryID),
		})
	}
	if ownerAccountID := strings.TrimSpace(share.OwnerAccountID); ownerAccountID != "" {
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: catalog.RelationshipDSSharedDirectoryTargetsOwnerAccount,
			SourceResourceID: sourceID,
			TargetResourceID: ownerAccountID,
			TargetType:       catalog.ResourceTypeAWSAccount,
			Attributes:       map[string]any{"owner_account_id": ownerAccountID},
			SourceRecordID:   relationshipRecordID(sourceID, catalog.RelationshipDSSharedDirectoryTargetsOwnerAccount, ownerAccountID),
		})
	}
	return relationships
}
