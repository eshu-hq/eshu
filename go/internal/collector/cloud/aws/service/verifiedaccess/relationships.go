// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package verifiedaccess

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// groupInInstanceRelationship records a Verified Access group's membership in
// its parent instance. The instance is keyed by the resource_id the instance
// node publishes (its synthesized partition-aware ARN, falling back to the bare
// id), so the edge joins the instance node exactly. It returns nil when either
// endpoint identity is missing.
func groupInInstanceRelationship(boundary aws.Boundary, group Group) *aws.RelationshipObservation {
	sourceID := groupResourceID(boundary, group)
	instanceID := strings.TrimSpace(group.InstanceID)
	if sourceID == "" || instanceID == "" {
		return nil
	}
	targetID := instanceResourceID(boundary, instanceID)
	if targetID == "" {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipVerifiedAccessGroupInInstance,
		SourceResourceID: sourceID,
		SourceARN:        arnOrEmpty(sourceID),
		TargetResourceID: targetID,
		TargetARN:        arnOrEmpty(targetID),
		TargetType:       aws.ResourceTypeVerifiedAccessInstance,
		SourceRecordID:   sourceID + "->" + aws.RelationshipVerifiedAccessGroupInInstance + ":" + targetID,
	}
}

// endpointInGroupRelationship records a Verified Access endpoint's membership in
// its parent group. The group is keyed by the resource_id the group node
// publishes (its ARN). It returns nil when either endpoint identity is missing.
func endpointInGroupRelationship(boundary aws.Boundary, endpoint Endpoint) *aws.RelationshipObservation {
	sourceID := endpointResourceID(boundary, endpoint)
	groupID := strings.TrimSpace(endpoint.GroupID)
	if sourceID == "" || groupID == "" {
		return nil
	}
	targetID := groupResourceID(boundary, Group{ID: groupID})
	if targetID == "" {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipVerifiedAccessEndpointInGroup,
		SourceResourceID: sourceID,
		SourceARN:        arnOrEmpty(sourceID),
		TargetResourceID: targetID,
		TargetARN:        arnOrEmpty(targetID),
		TargetType:       aws.ResourceTypeVerifiedAccessGroup,
		SourceRecordID:   sourceID + "->" + aws.RelationshipVerifiedAccessEndpointInGroup + ":" + targetID,
	}
}

// instanceTrustProviderRelationships records each Verified Access instance's
// attachment to a trust provider, keyed by the resource_id the trust-provider
// node publishes. It returns nil entries skipped, so the result holds only the
// relationships with both endpoints present.
func instanceTrustProviderRelationships(boundary aws.Boundary, instance Instance) []aws.RelationshipObservation {
	sourceID := instanceResourceID(boundary, instance.ID)
	if sourceID == "" {
		return nil
	}
	var relationships []aws.RelationshipObservation
	for _, trustProviderID := range instance.TrustProviderIDs {
		targetID := trustProviderResourceID(boundary, trustProviderID)
		if targetID == "" {
			continue
		}
		relationships = append(relationships, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipVerifiedAccessInstanceUsesTrustProvider,
			SourceResourceID: sourceID,
			SourceARN:        arnOrEmpty(sourceID),
			TargetResourceID: targetID,
			TargetARN:        arnOrEmpty(targetID),
			TargetType:       aws.ResourceTypeVerifiedAccessTrustProvider,
			SourceRecordID:   sourceID + "->" + aws.RelationshipVerifiedAccessInstanceUsesTrustProvider + ":" + targetID,
		})
	}
	return relationships
}

// endpointSubnetRelationships records a Verified Access endpoint's reported VPC
// subnet placements. AWS reports bare subnet ids, which match how the EC2
// scanner publishes its aws_ec2_subnet resource_id, so the edge keys the bare
// id and never dangles. Each id is skipped when empty.
func endpointSubnetRelationships(boundary aws.Boundary, endpoint Endpoint) []aws.RelationshipObservation {
	sourceID := endpointResourceID(boundary, endpoint)
	if sourceID == "" {
		return nil
	}
	var relationships []aws.RelationshipObservation
	for _, subnetID := range endpoint.SubnetIDs {
		subnetID = strings.TrimSpace(subnetID)
		if subnetID == "" {
			continue
		}
		relationships = append(relationships, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipVerifiedAccessEndpointUsesSubnet,
			SourceResourceID: sourceID,
			SourceARN:        arnOrEmpty(sourceID),
			TargetResourceID: subnetID,
			TargetType:       aws.ResourceTypeEC2Subnet,
			SourceRecordID:   sourceID + "->" + aws.RelationshipVerifiedAccessEndpointUsesSubnet + ":" + subnetID,
		})
	}
	return relationships
}

// endpointSecurityGroupRelationships records a Verified Access endpoint's
// reported security-group attachments. AWS reports bare security-group ids,
// which match how the EC2 scanner publishes its aws_ec2_security_group
// resource_id, so the edge keys the bare id and never dangles. Each id is
// skipped when empty.
func endpointSecurityGroupRelationships(boundary aws.Boundary, endpoint Endpoint) []aws.RelationshipObservation {
	sourceID := endpointResourceID(boundary, endpoint)
	if sourceID == "" {
		return nil
	}
	var relationships []aws.RelationshipObservation
	for _, groupID := range endpoint.SecurityGroupIDs {
		groupID = strings.TrimSpace(groupID)
		if groupID == "" {
			continue
		}
		relationships = append(relationships, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipVerifiedAccessEndpointUsesSecurityGroup,
			SourceResourceID: sourceID,
			SourceARN:        arnOrEmpty(sourceID),
			TargetResourceID: groupID,
			TargetType:       aws.ResourceTypeEC2SecurityGroup,
			SourceRecordID:   sourceID + "->" + aws.RelationshipVerifiedAccessEndpointUsesSecurityGroup + ":" + groupID,
		})
	}
	return relationships
}

// endpointACMCertificateRelationship records a Verified Access endpoint's
// reported public TLS certificate. AWS reports the ACM certificate ARN, which
// matches how the ACM scanner publishes its aws_acm_certificate resource_id, so
// the edge keys the ARN and never dangles. It returns nil when no certificate is
// reported or the reported value is not an ARN.
func endpointACMCertificateRelationship(boundary aws.Boundary, endpoint Endpoint) *aws.RelationshipObservation {
	certARN := strings.TrimSpace(endpoint.DomainCertificateARN)
	if certARN == "" || !isARN(certARN) {
		return nil
	}
	sourceID := endpointResourceID(boundary, endpoint)
	if sourceID == "" {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipVerifiedAccessEndpointUsesACMCertificate,
		SourceResourceID: sourceID,
		SourceARN:        arnOrEmpty(sourceID),
		TargetResourceID: certARN,
		TargetARN:        certARN,
		TargetType:       aws.ResourceTypeACMCertificate,
		SourceRecordID:   sourceID + "->" + aws.RelationshipVerifiedAccessEndpointUsesACMCertificate + ":" + certARN,
	}
}

// arnOrEmpty returns value when it is ARN-shaped, otherwise "". It populates the
// source/target ARN field only for ARN-shaped identifiers, leaving bare ids
// (subnet-..., sg-...) without a synthetic ARN.
func arnOrEmpty(value string) string {
	if isARN(value) {
		return strings.TrimSpace(value)
	}
	return ""
}
