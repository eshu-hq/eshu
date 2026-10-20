// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workspaces

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// workspaceInDirectoryRelationship records a WorkSpace's membership in its
// parent WorkSpaces directory. The target is keyed by the directory node's
// published resource_id (the synthesized WorkSpaces directory ARN) so the edge
// joins the directory node this scanner publishes, not the Directory Service
// node. It returns nil when either endpoint identity is missing.
func workspaceInDirectoryRelationship(
	boundary awscloud.Boundary,
	workspace Workspace,
) *awscloud.RelationshipObservation {
	sourceID := workspaceResourceID(boundary, workspace)
	directoryID := strings.TrimSpace(workspace.DirectoryID)
	if sourceID == "" || directoryID == "" {
		return nil
	}
	targetID := directoryResourceID(boundary, Directory{ID: directoryID})
	if targetID == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipWorkSpacesWorkspaceInDirectory,
		SourceResourceID: sourceID,
		SourceARN:        arnOrEmpty(sourceID),
		TargetResourceID: targetID,
		TargetARN:        arnOrEmpty(targetID),
		TargetType:       awscloud.ResourceTypeWorkSpacesDirectory,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipWorkSpacesWorkspaceInDirectory + ":" + targetID,
	}
}

// workspaceUsesBundleRelationship records a WorkSpace's dependency on the bundle
// it was created from. The target is the WorkSpaces bundle node keyed by its
// published resource_id (the synthesized WorkSpaces bundle ARN). It returns nil
// when either endpoint identity is missing.
func workspaceUsesBundleRelationship(
	boundary awscloud.Boundary,
	workspace Workspace,
) *awscloud.RelationshipObservation {
	sourceID := workspaceResourceID(boundary, workspace)
	bundleID := strings.TrimSpace(workspace.BundleID)
	if sourceID == "" || bundleID == "" {
		return nil
	}
	targetID := bundleResourceID(boundary, Bundle{ID: bundleID})
	if targetID == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipWorkSpacesWorkspaceUsesBundle,
		SourceResourceID: sourceID,
		SourceARN:        arnOrEmpty(sourceID),
		TargetResourceID: targetID,
		TargetARN:        arnOrEmpty(targetID),
		TargetType:       awscloud.ResourceTypeWorkSpacesBundle,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipWorkSpacesWorkspaceUsesBundle + ":" + targetID,
	}
}

// workspaceUsesKMSKeyRelationship records a WorkSpace's reported volume
// encryption KMS key dependency. WorkSpaces reports a key ARN, which matches how
// the KMS scanner publishes its key resource_id (bare id or key ARN). It
// returns nil when no key is reported.
func workspaceUsesKMSKeyRelationship(
	boundary awscloud.Boundary,
	workspace Workspace,
) *awscloud.RelationshipObservation {
	targetID := strings.TrimSpace(workspace.VolumeEncryptionKey)
	if targetID == "" {
		return nil
	}
	sourceID := workspaceResourceID(boundary, workspace)
	if sourceID == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipWorkSpacesWorkspaceUsesKMSKey,
		SourceResourceID: sourceID,
		SourceARN:        arnOrEmpty(sourceID),
		TargetResourceID: targetID,
		TargetARN:        arnOrEmpty(targetID),
		TargetType:       awscloud.ResourceTypeKMSKey,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipWorkSpacesWorkspaceUsesKMSKey + ":" + targetID,
	}
}

// directoryRelationships records every cross-service edge a WorkSpaces directory
// reports: the underlying Directory Service directory, the placement subnets,
// the assigned security group, the WorkSpaces IAM role, and the associated IP
// access control groups. It returns nil when the directory has no usable
// identity.
func directoryRelationships(
	boundary awscloud.Boundary,
	directory Directory,
) []awscloud.RelationshipObservation {
	sourceID := directoryResourceID(boundary, directory)
	if sourceID == "" {
		return nil
	}
	sourceARN := arnOrEmpty(sourceID)
	var observations []awscloud.RelationshipObservation
	if rel := directoryUsesDSDirectoryRelationship(boundary, sourceID, sourceARN, directory); rel != nil {
		observations = append(observations, *rel)
	}
	observations = append(observations, directorySubnetRelationships(boundary, sourceID, sourceARN, directory)...)
	if rel := directoryUsesSecurityGroupRelationship(boundary, sourceID, sourceARN, directory); rel != nil {
		observations = append(observations, *rel)
	}
	if rel := directoryUsesIAMRoleRelationship(boundary, sourceID, sourceARN, directory); rel != nil {
		observations = append(observations, *rel)
	}
	observations = append(observations, directoryIPGroupRelationships(boundary, sourceID, sourceARN, directory)...)
	return observations
}

// directoryUsesDSDirectoryRelationship records the WorkSpaces directory's link
// to the underlying AWS Directory Service directory. The target is the BARE
// directory id (for example "d-1234567890") the ds scanner publishes as its
// resource_id, so the edge joins the DS node, not a synthesized ARN.
func directoryUsesDSDirectoryRelationship(
	boundary awscloud.Boundary,
	sourceID, sourceARN string,
	directory Directory,
) *awscloud.RelationshipObservation {
	targetID := strings.TrimSpace(directory.ID)
	if targetID == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipWorkSpacesDirectoryUsesDSDirectory,
		SourceResourceID: sourceID,
		SourceARN:        sourceARN,
		TargetResourceID: targetID,
		TargetType:       awscloud.ResourceTypeDSDirectory,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipWorkSpacesDirectoryUsesDSDirectory + ":" + targetID,
	}
}

// directorySubnetRelationships records a WorkSpaces directory's placement in
// each reported VPC subnet. The target is the bare subnet id the ec2 scanner
// publishes.
func directorySubnetRelationships(
	boundary awscloud.Boundary,
	sourceID, sourceARN string,
	directory Directory,
) []awscloud.RelationshipObservation {
	subnets := cloneStrings(directory.SubnetIDs)
	if len(subnets) == 0 {
		return nil
	}
	observations := make([]awscloud.RelationshipObservation, 0, len(subnets))
	for _, subnet := range subnets {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipWorkSpacesDirectoryInSubnet,
			SourceResourceID: sourceID,
			SourceARN:        sourceARN,
			TargetResourceID: subnet,
			TargetType:       awscloud.ResourceTypeEC2Subnet,
			SourceRecordID:   sourceID + "->" + awscloud.RelationshipWorkSpacesDirectoryInSubnet + ":" + subnet,
		})
	}
	return observations
}

// directoryUsesSecurityGroupRelationship records the WorkSpaces security group
// assigned to new WorkSpaces in the directory. The target is the bare security
// group id the ec2 scanner publishes.
func directoryUsesSecurityGroupRelationship(
	boundary awscloud.Boundary,
	sourceID, sourceARN string,
	directory Directory,
) *awscloud.RelationshipObservation {
	targetID := strings.TrimSpace(directory.WorkspaceSecurityGroupID)
	if targetID == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipWorkSpacesDirectoryUsesSecurityGroup,
		SourceResourceID: sourceID,
		SourceARN:        sourceARN,
		TargetResourceID: targetID,
		TargetType:       awscloud.ResourceTypeEC2SecurityGroup,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipWorkSpacesDirectoryUsesSecurityGroup + ":" + targetID,
	}
}

// directoryUsesIAMRoleRelationship records the IAM role WorkSpaces assumes to
// call other services on the account's behalf. WorkSpaces reports the role ARN,
// which matches how the iam scanner publishes its role resource_id. It returns
// nil when no role is reported.
func directoryUsesIAMRoleRelationship(
	boundary awscloud.Boundary,
	sourceID, sourceARN string,
	directory Directory,
) *awscloud.RelationshipObservation {
	targetID := strings.TrimSpace(directory.IamRoleID)
	if targetID == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipWorkSpacesDirectoryUsesIAMRole,
		SourceResourceID: sourceID,
		SourceARN:        sourceARN,
		TargetResourceID: targetID,
		TargetARN:        arnOrEmpty(targetID),
		TargetType:       awscloud.ResourceTypeIAMRole,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipWorkSpacesDirectoryUsesIAMRole + ":" + targetID,
	}
}

// directoryIPGroupRelationships records a WorkSpaces directory's association
// with each IP access control group. The target is the WorkSpaces IP-group node
// keyed by its published resource_id (the synthesized WorkSpaces IP-group ARN).
func directoryIPGroupRelationships(
	boundary awscloud.Boundary,
	sourceID, sourceARN string,
	directory Directory,
) []awscloud.RelationshipObservation {
	groups := cloneStrings(directory.IPGroupIDs)
	if len(groups) == 0 {
		return nil
	}
	observations := make([]awscloud.RelationshipObservation, 0, len(groups))
	for _, group := range groups {
		targetID := ipGroupResourceID(boundary, IPGroup{ID: group})
		if targetID == "" {
			continue
		}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipWorkSpacesDirectoryUsesIPGroup,
			SourceResourceID: sourceID,
			SourceARN:        sourceARN,
			TargetResourceID: targetID,
			TargetARN:        arnOrEmpty(targetID),
			TargetType:       awscloud.ResourceTypeWorkSpacesIPGroup,
			SourceRecordID:   sourceID + "->" + awscloud.RelationshipWorkSpacesDirectoryUsesIPGroup + ":" + targetID,
		})
	}
	return observations
}

// arnOrEmpty returns value when it is ARN-shaped, otherwise "". It keeps a bare
// id from being copied into a target_arn/source_arn field where an ARN is
// expected.
func arnOrEmpty(value string) string {
	if isARN(value) {
		return strings.TrimSpace(value)
	}
	return ""
}
