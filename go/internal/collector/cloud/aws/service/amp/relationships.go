// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package amp

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// workspaceKMSRelationship records an AMP workspace's reported customer-managed
// KMS encryption key dependency. AWS reports a key ARN, which matches how the
// KMS scanner publishes its key resource_id, so the edge targets aws_kms_key. It
// returns nil when no key is reported.
func workspaceKMSRelationship(boundary aws.Boundary, workspace Workspace) *aws.RelationshipObservation {
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
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipAMPWorkspaceUsesKMSKey,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(workspace.ARN),
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       aws.ResourceTypeKMSKey,
		SourceRecordID:   sourceID + "->" + aws.RelationshipAMPWorkspaceUsesKMSKey + ":" + targetID,
	}
}

// namespaceInWorkspaceRelationship records a rule-groups namespace's membership
// in its parent workspace. workspaceID is the resource_id the workspace node
// publishes (its ARN when available), so the edge joins the workspace node
// exactly. It returns nil when either endpoint identity is missing.
func namespaceInWorkspaceRelationship(
	boundary aws.Boundary,
	workspaceID string,
	namespace RuleGroupsNamespace,
) *aws.RelationshipObservation {
	namespaceID := namespaceResourceID(namespace)
	workspaceID = strings.TrimSpace(workspaceID)
	if namespaceID == "" || workspaceID == "" {
		return nil
	}
	targetARN := ""
	if isARN(workspaceID) {
		targetARN = workspaceID
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipAMPRuleGroupsNamespaceInWorkspace,
		SourceResourceID: namespaceID,
		SourceARN:        strings.TrimSpace(namespace.ARN),
		TargetResourceID: workspaceID,
		TargetARN:        targetARN,
		TargetType:       aws.ResourceTypeAMPWorkspace,
		SourceRecordID:   namespaceID + "->" + aws.RelationshipAMPRuleGroupsNamespaceInWorkspace + ":" + workspaceID,
	}
}

// scraperRelationships records an AMP scraper's reported dependencies: the EKS
// source cluster, the destination workspace, and the EKS VPC configuration
// subnets and security groups. Each edge is emitted only when AWS reports the
// dependency, so absent endpoints never dangle the graph. The EKS cluster ARN,
// destination workspace ARN, bare subnet ids, and bare security-group ids each
// match the resource_id their owning scanner publishes.
func scraperRelationships(boundary aws.Boundary, scraper Scraper) []aws.RelationshipObservation {
	scraperID := scraperResourceID(scraper)
	if scraperID == "" {
		return nil
	}
	var observations []aws.RelationshipObservation
	scraperARN := strings.TrimSpace(scraper.ARN)

	if clusterARN := strings.TrimSpace(scraper.SourceEKSClusterARN); clusterARN != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipAMPScraperScrapesEKSCluster,
			SourceResourceID: scraperID,
			SourceARN:        scraperARN,
			TargetResourceID: clusterARN,
			TargetARN:        clusterARN,
			TargetType:       aws.ResourceTypeEKSCluster,
			SourceRecordID:   scraperID + "->" + aws.RelationshipAMPScraperScrapesEKSCluster + ":" + clusterARN,
		})
	}

	if workspaceARN := strings.TrimSpace(scraper.DestinationWorkspaceARN); workspaceARN != "" {
		targetARN := ""
		if isARN(workspaceARN) {
			targetARN = workspaceARN
		}
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipAMPScraperSendsToWorkspace,
			SourceResourceID: scraperID,
			SourceARN:        scraperARN,
			TargetResourceID: workspaceARN,
			TargetARN:        targetARN,
			TargetType:       aws.ResourceTypeAMPWorkspace,
			SourceRecordID:   scraperID + "->" + aws.RelationshipAMPScraperSendsToWorkspace + ":" + workspaceARN,
		})
	}

	for _, subnetID := range cloneStrings(scraper.SubnetIDs) {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipAMPScraperUsesSubnet,
			SourceResourceID: scraperID,
			SourceARN:        scraperARN,
			TargetResourceID: subnetID,
			TargetType:       aws.ResourceTypeEC2Subnet,
			SourceRecordID:   scraperID + "->" + aws.RelationshipAMPScraperUsesSubnet + ":" + subnetID,
		})
	}

	for _, groupID := range cloneStrings(scraper.SecurityGroupIDs) {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipAMPScraperUsesSecurityGroup,
			SourceResourceID: scraperID,
			SourceARN:        scraperARN,
			TargetResourceID: groupID,
			TargetType:       aws.ResourceTypeEC2SecurityGroup,
			SourceRecordID:   scraperID + "->" + aws.RelationshipAMPScraperUsesSecurityGroup + ":" + groupID,
		})
	}

	return observations
}
