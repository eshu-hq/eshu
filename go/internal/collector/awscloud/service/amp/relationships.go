// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package amp

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// workspaceKMSRelationship records an AMP workspace's reported customer-managed
// KMS encryption key dependency. AWS reports a key ARN, which matches how the
// KMS scanner publishes its key resource_id, so the edge targets aws_kms_key. It
// returns nil when no key is reported.
func workspaceKMSRelationship(boundary awscloud.Boundary, workspace Workspace) *awscloud.RelationshipObservation {
	targetID := strings.TrimSpace(workspace.KMSKeyARN)
	if targetID == "" {
		return nil
	}
	sourceID := workspaceResourceID(workspace)
	if sourceID == "" {
		return nil
	}
	targetARN := ""
	if isARN(targetID) {
		targetARN = targetID
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipAMPWorkspaceUsesKMSKey,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(workspace.ARN),
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeKMSKey,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipAMPWorkspaceUsesKMSKey + ":" + targetID,
	}
}

// namespaceInWorkspaceRelationship records a rule-groups namespace's membership
// in its parent workspace. workspaceID is the resource_id the workspace node
// publishes (its ARN when available), so the edge joins the workspace node
// exactly. It returns nil when either endpoint identity is missing.
func namespaceInWorkspaceRelationship(
	boundary awscloud.Boundary,
	workspaceID string,
	namespace RuleGroupsNamespace,
) *awscloud.RelationshipObservation {
	namespaceID := namespaceResourceID(namespace)
	workspaceID = strings.TrimSpace(workspaceID)
	if namespaceID == "" || workspaceID == "" {
		return nil
	}
	targetARN := ""
	if isARN(workspaceID) {
		targetARN = workspaceID
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipAMPRuleGroupsNamespaceInWorkspace,
		SourceResourceID: namespaceID,
		SourceARN:        strings.TrimSpace(namespace.ARN),
		TargetResourceID: workspaceID,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeAMPWorkspace,
		SourceRecordID:   namespaceID + "->" + awscloud.RelationshipAMPRuleGroupsNamespaceInWorkspace + ":" + workspaceID,
	}
}

// scraperRelationships records an AMP scraper's reported dependencies: the EKS
// source cluster, the destination workspace, and the EKS VPC configuration
// subnets and security groups. Each edge is emitted only when AWS reports the
// dependency, so absent endpoints never dangle the graph. The EKS cluster ARN,
// destination workspace ARN, bare subnet ids, and bare security-group ids each
// match the resource_id their owning scanner publishes.
func scraperRelationships(boundary awscloud.Boundary, scraper Scraper) []awscloud.RelationshipObservation {
	scraperID := scraperResourceID(scraper)
	if scraperID == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	scraperARN := strings.TrimSpace(scraper.ARN)

	if clusterARN := strings.TrimSpace(scraper.SourceEKSClusterARN); clusterARN != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAMPScraperScrapesEKSCluster,
			SourceResourceID: scraperID,
			SourceARN:        scraperARN,
			TargetResourceID: clusterARN,
			TargetARN:        clusterARN,
			TargetType:       awscloud.ResourceTypeEKSCluster,
			SourceRecordID:   scraperID + "->" + awscloud.RelationshipAMPScraperScrapesEKSCluster + ":" + clusterARN,
		})
	}

	if workspaceARN := strings.TrimSpace(scraper.DestinationWorkspaceARN); workspaceARN != "" {
		targetARN := ""
		if isARN(workspaceARN) {
			targetARN = workspaceARN
		}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAMPScraperSendsToWorkspace,
			SourceResourceID: scraperID,
			SourceARN:        scraperARN,
			TargetResourceID: workspaceARN,
			TargetARN:        targetARN,
			TargetType:       awscloud.ResourceTypeAMPWorkspace,
			SourceRecordID:   scraperID + "->" + awscloud.RelationshipAMPScraperSendsToWorkspace + ":" + workspaceARN,
		})
	}

	for _, subnetID := range cloneStrings(scraper.SubnetIDs) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAMPScraperUsesSubnet,
			SourceResourceID: scraperID,
			SourceARN:        scraperARN,
			TargetResourceID: subnetID,
			TargetType:       awscloud.ResourceTypeEC2Subnet,
			SourceRecordID:   scraperID + "->" + awscloud.RelationshipAMPScraperUsesSubnet + ":" + subnetID,
		})
	}

	for _, groupID := range cloneStrings(scraper.SecurityGroupIDs) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAMPScraperUsesSecurityGroup,
			SourceResourceID: scraperID,
			SourceARN:        scraperARN,
			TargetResourceID: groupID,
			TargetType:       awscloud.ResourceTypeEC2SecurityGroup,
			SourceRecordID:   scraperID + "->" + awscloud.RelationshipAMPScraperUsesSecurityGroup + ":" + groupID,
		})
	}

	return observations
}
