// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ds

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// directoryRelationships returns the VPC and subnet edges reported by one
// directory. Each edge sets a non-empty target_type and a target_resource_id
// that matches the target scanner's resource_id: VPC and subnet edges use the
// bare AWS ID (joining the VPC scanner's aws_ec2_vpc and aws_ec2_subnet
// resources).
func directoryRelationships(boundary aws.Boundary, directory Directory) []aws.RelationshipObservation {
	sourceID := strings.TrimSpace(directory.ID)
	if sourceID == "" {
		return nil
	}
	var relationships []aws.RelationshipObservation

	if vpcID := strings.TrimSpace(directory.VPCID); vpcID != "" {
		relationships = append(relationships, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipDSDirectoryInVPC,
			SourceResourceID: sourceID,
			TargetResourceID: vpcID,
			TargetType:       aws.ResourceTypeEC2VPC,
			Attributes:       map[string]any{"vpc_id": vpcID},
			SourceRecordID:   relationshipRecordID(sourceID, aws.RelationshipDSDirectoryInVPC, vpcID),
		})
	}
	for _, subnetID := range cloneStrings(directory.SubnetIDs) {
		relationships = append(relationships, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipDSDirectoryInSubnet,
			SourceResourceID: sourceID,
			TargetResourceID: subnetID,
			TargetType:       aws.ResourceTypeEC2Subnet,
			Attributes:       map[string]any{"subnet_id": subnetID},
			SourceRecordID:   relationshipRecordID(sourceID, aws.RelationshipDSDirectoryInSubnet, subnetID),
		})
	}
	return relationships
}

// trustRelationships returns the trust-to-directory edge for one trust. The
// target is the bare directory id so it joins the directory resource fact emitted
// in the same scan.
func trustRelationships(boundary aws.Boundary, trust Trust) []aws.RelationshipObservation {
	sourceID := strings.TrimSpace(trust.ID)
	directoryID := strings.TrimSpace(trust.DirectoryID)
	if sourceID == "" || directoryID == "" {
		return nil
	}
	return []aws.RelationshipObservation{{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipDSTrustTargetsDirectory,
		SourceResourceID: sourceID,
		TargetResourceID: directoryID,
		TargetType:       aws.ResourceTypeDSDirectory,
		Attributes:       map[string]any{"directory_id": directoryID},
		SourceRecordID:   relationshipRecordID(sourceID, aws.RelationshipDSTrustTargetsDirectory, directoryID),
	}}
}

// sharedDirectoryRelationships returns the owner-directory and owner-account
// edges for one shared-directory invitation. The owner-directory edge targets the
// bare owner directory id (joining the directory resource fact when in scope),
// and the owner-account edge targets the bare 12-digit account id; no ARN is
// synthesized for either target.
func sharedDirectoryRelationships(
	boundary aws.Boundary,
	share SharedDirectory,
	directoryIDs map[string]struct{},
) []aws.RelationshipObservation {
	sourceID := sharedDirectoryResourceID(share)
	if sourceID == "" {
		return nil
	}
	var relationships []aws.RelationshipObservation

	if ownerDirectoryID := strings.TrimSpace(share.OwnerDirectoryID); ownerDirectoryID != "" {
		_, inScope := directoryIDs[ownerDirectoryID]
		relationships = append(relationships, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipDSSharedDirectoryTargetsOwnerDirectory,
			SourceResourceID: sourceID,
			TargetResourceID: ownerDirectoryID,
			TargetType:       aws.ResourceTypeDSDirectory,
			Attributes: map[string]any{
				"owner_directory_id": ownerDirectoryID,
				"in_scope":           inScope,
			},
			SourceRecordID: relationshipRecordID(sourceID, aws.RelationshipDSSharedDirectoryTargetsOwnerDirectory, ownerDirectoryID),
		})
	}
	if ownerAccountID := strings.TrimSpace(share.OwnerAccountID); ownerAccountID != "" {
		relationships = append(relationships, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipDSSharedDirectoryTargetsOwnerAccount,
			SourceResourceID: sourceID,
			TargetResourceID: ownerAccountID,
			TargetType:       aws.ResourceTypeAWSAccount,
			Attributes:       map[string]any{"owner_account_id": ownerAccountID},
			SourceRecordID:   relationshipRecordID(sourceID, aws.RelationshipDSSharedDirectoryTargetsOwnerAccount, ownerAccountID),
		})
	}
	return relationships
}
