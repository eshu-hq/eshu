// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package grafana

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// workspaceRelationships returns every outgoing edge for one Managed Grafana
// workspace. Each edge is sourced on the same identifier the workspace resource
// publishes as its resource_id (the workspace ARN, falling back to the bare
// workspace id) so the source node and the edge source agree. An edge is emitted
// only when AWS reports a non-empty, well-shaped target identifier that matches
// how the target scanner publishes its resource_id, otherwise the edge is
// skipped rather than dangled.
func workspaceRelationships(boundary awscloud.Boundary, workspace Workspace) []awscloud.RelationshipObservation {
	sourceID := workspaceResourceID(workspace)
	if sourceID == "" {
		return nil
	}
	sourceARN := strings.TrimSpace(workspace.ARN)
	var observations []awscloud.RelationshipObservation

	if rel, ok := workspaceIAMRoleRelationship(boundary, workspace, sourceID, sourceARN); ok {
		observations = append(observations, rel)
	}
	observations = append(observations, workspaceSubnetRelationships(boundary, workspace, sourceID, sourceARN)...)
	observations = append(observations, workspaceSecurityGroupRelationships(boundary, workspace, sourceID, sourceARN)...)

	return observations
}

// workspaceIAMRoleRelationship records the workspace IAM role the workspace
// assumes to read configured data sources. AWS reports a role ARN, which is how
// the IAM scanner publishes its role resource_id, so the edge joins exactly. It
// returns false when no role ARN is reported.
func workspaceIAMRoleRelationship(
	boundary awscloud.Boundary,
	workspace Workspace,
	sourceID string,
	sourceARN string,
) (awscloud.RelationshipObservation, bool) {
	roleARN := strings.TrimSpace(workspace.WorkspaceRoleARN)
	if !isARN(roleARN) {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipGrafanaWorkspaceUsesIAMRole,
		SourceResourceID: sourceID,
		SourceARN:        sourceARN,
		TargetResourceID: roleARN,
		TargetARN:        roleARN,
		TargetType:       awscloud.ResourceTypeIAMRole,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipGrafanaWorkspaceUsesIAMRole + ":" + roleARN,
	}, true
}

// workspaceSubnetRelationships records each VPC subnet the workspace attaches to
// from its vpcConfiguration. The EC2 scanner publishes a subnet resource_id as
// the bare subnet id (subnet-...), so the edge targets the bare id directly. The
// id list is de-duplicated so a repeated subnet id does not create duplicate
// edges.
func workspaceSubnetRelationships(
	boundary awscloud.Boundary,
	workspace Workspace,
	sourceID string,
	sourceARN string,
) []awscloud.RelationshipObservation {
	subnetIDs := dedupeStrings(workspace.SubnetIDs)
	observations := make([]awscloud.RelationshipObservation, 0, len(subnetIDs))
	for _, subnetID := range subnetIDs {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipGrafanaWorkspaceInSubnet,
			SourceResourceID: sourceID,
			SourceARN:        sourceARN,
			TargetResourceID: subnetID,
			TargetType:       awscloud.ResourceTypeEC2Subnet,
			SourceRecordID:   sourceID + "->" + awscloud.RelationshipGrafanaWorkspaceInSubnet + ":" + subnetID,
		})
	}
	return observations
}

// workspaceSecurityGroupRelationships records each VPC security group the
// workspace attaches to from its vpcConfiguration. The EC2 scanner publishes a
// security-group resource_id as the bare group id (sg-...), so the edge targets
// the bare id directly. The id list is de-duplicated so a repeated group id does
// not create duplicate edges.
func workspaceSecurityGroupRelationships(
	boundary awscloud.Boundary,
	workspace Workspace,
	sourceID string,
	sourceARN string,
) []awscloud.RelationshipObservation {
	groupIDs := dedupeStrings(workspace.SecurityGroupIDs)
	observations := make([]awscloud.RelationshipObservation, 0, len(groupIDs))
	for _, groupID := range groupIDs {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipGrafanaWorkspaceUsesSecurityGroup,
			SourceResourceID: sourceID,
			SourceARN:        sourceARN,
			TargetResourceID: groupID,
			TargetType:       awscloud.ResourceTypeEC2SecurityGroup,
			SourceRecordID:   sourceID + "->" + awscloud.RelationshipGrafanaWorkspaceUsesSecurityGroup + ":" + groupID,
		})
	}
	return observations
}
