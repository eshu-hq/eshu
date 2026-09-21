// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package opensearchserverless

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// collectionKMSRelationship records the customer-managed KMS encryption key an
// OpenSearch Serverless collection is assigned through its matching encryption
// security policy. bindings carries the policy-parsed key ARNs and collection
// patterns (never the policy body). AWS reports a key ARN, which matches how the
// KMS scanner publishes its key resource_id (bare key id or ARN). It returns nil
// when the collection has no identity or no encryption policy assigns it a
// customer-managed key.
func collectionKMSRelationship(
	boundary aws.Boundary,
	collection Collection,
	bindings []EncryptionKeyBinding,
) *aws.RelationshipObservation {
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
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipOpenSearchServerlessCollectionUsesKMSKey,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(collection.ARN),
		TargetResourceID: keyARN,
		TargetARN:        targetARN,
		TargetType:       aws.ResourceTypeKMSKey,
		Attributes:       attributes,
		SourceRecordID:   sourceID + "->" + aws.RelationshipOpenSearchServerlessCollectionUsesKMSKey + ":" + keyARN,
	}
}

// vpcEndpointRelationships returns the reported network dependency edges for one
// managed VPC endpoint: its VPC, its subnets, and its security groups. AWS
// reports bare EC2 ids for all three, matching how the EC2 scanner publishes the
// vpc-…, subnet-…, and sg-… resource_ids. It returns nil when the endpoint has no
// resolvable identity.
func vpcEndpointRelationships(boundary aws.Boundary, endpoint VPCEndpoint) []aws.RelationshipObservation {
	endpointID := vpcEndpointResourceID(endpoint)
	if endpointID == "" {
		return nil
	}
	var observations []aws.RelationshipObservation
	if vpcID := strings.TrimSpace(endpoint.VPCID); vpcID != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipOpenSearchServerlessVPCEndpointInVPC,
			SourceResourceID: endpointID,
			TargetResourceID: vpcID,
			TargetType:       aws.ResourceTypeEC2VPC,
			SourceRecordID:   endpointID + "#vpc#" + vpcID,
		})
	}
	for _, subnetID := range cloneStrings(endpoint.SubnetIDs) {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipOpenSearchServerlessVPCEndpointInSubnet,
			SourceResourceID: endpointID,
			TargetResourceID: subnetID,
			TargetType:       aws.ResourceTypeEC2Subnet,
			SourceRecordID:   endpointID + "#subnet#" + subnetID,
		})
	}
	for _, groupID := range cloneStrings(endpoint.SecurityGroupIDs) {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipOpenSearchServerlessVPCEndpointUsesSecurityGroup,
			SourceResourceID: endpointID,
			TargetResourceID: groupID,
			TargetType:       aws.ResourceTypeEC2SecurityGroup,
			SourceRecordID:   endpointID + "#security-group#" + groupID,
		})
	}
	return observations
}
