// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package opensearchserverless

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// collectionKMSRelationship records the customer-managed KMS encryption key an
// OpenSearch Serverless collection is assigned through its matching encryption
// security policy. bindings carries the policy-parsed key ARNs and collection
// patterns (never the policy body). AWS reports a key ARN, which matches how the
// KMS scanner publishes its key resource_id (bare key id or ARN). It returns nil
// when the collection has no identity or no encryption policy assigns it a
// customer-managed key.
func collectionKMSRelationship(
	boundary awscloud.Boundary,
	collection Collection,
	bindings []EncryptionKeyBinding,
) *awscloud.RelationshipObservation {
	sourceID := collectionResourceID(collection)
	if sourceID == "" {
		return nil
	}
	keyARN, policyName := matchEncryptionKey(bindings, collection.Name)
	if keyARN == "" {
		return nil
	}
	targetARN := ""
	if isARN(keyARN) {
		targetARN = keyARN
	}
	attributes := map[string]any{}
	if policyName != "" {
		attributes["encryption_policy_name"] = policyName
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipOpenSearchServerlessCollectionUsesKMSKey,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(collection.ARN),
		TargetResourceID: keyARN,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeKMSKey,
		Attributes:       attributes,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipOpenSearchServerlessCollectionUsesKMSKey + ":" + keyARN,
	}
}

// vpcEndpointRelationships returns the reported network dependency edges for one
// managed VPC endpoint: its VPC, its subnets, and its security groups. AWS
// reports bare EC2 ids for all three, matching how the EC2 scanner publishes the
// vpc-…, subnet-…, and sg-… resource_ids. It returns nil when the endpoint has no
// resolvable identity.
func vpcEndpointRelationships(boundary awscloud.Boundary, endpoint VPCEndpoint) []awscloud.RelationshipObservation {
	endpointID := vpcEndpointResourceID(endpoint)
	if endpointID == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	if vpcID := strings.TrimSpace(endpoint.VPCID); vpcID != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipOpenSearchServerlessVPCEndpointInVPC,
			SourceResourceID: endpointID,
			TargetResourceID: vpcID,
			TargetType:       awscloud.ResourceTypeEC2VPC,
			SourceRecordID:   endpointID + "#vpc#" + vpcID,
		})
	}
	for _, subnetID := range cloneStrings(endpoint.SubnetIDs) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipOpenSearchServerlessVPCEndpointInSubnet,
			SourceResourceID: endpointID,
			TargetResourceID: subnetID,
			TargetType:       awscloud.ResourceTypeEC2Subnet,
			SourceRecordID:   endpointID + "#subnet#" + subnetID,
		})
	}
	for _, groupID := range cloneStrings(endpoint.SecurityGroupIDs) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipOpenSearchServerlessVPCEndpointUsesSecurityGroup,
			SourceResourceID: endpointID,
			TargetResourceID: groupID,
			TargetType:       awscloud.ResourceTypeEC2SecurityGroup,
			SourceRecordID:   endpointID + "#security-group#" + groupID,
		})
	}
	return observations
}
