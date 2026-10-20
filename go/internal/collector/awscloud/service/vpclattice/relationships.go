// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vpclattice

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// serviceNetworkVPCRelationship records a service network's association with a
// VPC. AWS reports the bare VPC id, which is the resource_id the EC2 scanner
// publishes for a VPC node, so the edge joins the VPC node exactly. It returns
// nil when either endpoint identity is missing.
func serviceNetworkVPCRelationship(
	boundary awscloud.Boundary,
	network ServiceNetwork,
	association VPCAssociation,
) *awscloud.RelationshipObservation {
	sourceID := serviceNetworkResourceID(network)
	vpcID := strings.TrimSpace(association.VPCID)
	if sourceID == "" || vpcID == "" {
		return nil
	}
	attributes := map[string]any{}
	if status := strings.TrimSpace(association.Status); status != "" {
		attributes["status"] = status
	}
	if id := strings.TrimSpace(association.ID); id != "" {
		attributes["association_id"] = id
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipVPCLatticeServiceNetworkAssociatesVPC,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(network.ARN),
		TargetResourceID: vpcID,
		TargetType:       awscloud.ResourceTypeEC2VPC,
		Attributes:       attributesOrNil(attributes),
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipVPCLatticeServiceNetworkAssociatesVPC + ":" + vpcID,
	}
}

// serviceNetworkServiceRelationship records a service network's association
// with a VPC Lattice service. AWS reports the service ARN, which is the
// resource_id this scanner publishes for a service node. It returns nil when
// either endpoint identity is missing.
func serviceNetworkServiceRelationship(
	boundary awscloud.Boundary,
	network ServiceNetwork,
	association ServiceAssociation,
) *awscloud.RelationshipObservation {
	sourceID := serviceNetworkResourceID(network)
	targetID := firstNonEmpty(association.ServiceARN, association.ServiceID)
	if sourceID == "" || targetID == "" {
		return nil
	}
	targetARN := ""
	if isARN(targetID) {
		targetARN = targetID
	}
	attributes := map[string]any{}
	if status := strings.TrimSpace(association.Status); status != "" {
		attributes["status"] = status
	}
	if id := strings.TrimSpace(association.ID); id != "" {
		attributes["association_id"] = id
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipVPCLatticeServiceNetworkAssociatesService,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(network.ARN),
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeVPCLatticeService,
		Attributes:       attributesOrNil(attributes),
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipVPCLatticeServiceNetworkAssociatesService + ":" + targetID,
	}
}

// listenerInServiceRelationship records a listener's membership in its parent
// service. The service ARN is the resource_id the service node publishes, so
// the edge joins the service node exactly. It returns nil when either endpoint
// identity is missing.
func listenerInServiceRelationship(
	boundary awscloud.Boundary,
	service Service,
	listener Listener,
) *awscloud.RelationshipObservation {
	sourceID := listenerResourceID(listener)
	targetID := serviceResourceID(service)
	if sourceID == "" || targetID == "" {
		return nil
	}
	targetARN := ""
	if isARN(targetID) {
		targetARN = targetID
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipVPCLatticeListenerInService,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(listener.ARN),
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeVPCLatticeService,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipVPCLatticeListenerInService + ":" + targetID,
	}
}

// serviceCertificateRelationship records a service's ACM certificate binding.
// AWS reports the certificate ARN, which matches the resource_id the ACM
// scanner publishes for a certificate. It returns nil when no certificate is
// configured.
func serviceCertificateRelationship(
	boundary awscloud.Boundary,
	service Service,
) *awscloud.RelationshipObservation {
	certARN := strings.TrimSpace(service.CertificateARN)
	if certARN == "" {
		return nil
	}
	sourceID := serviceResourceID(service)
	if sourceID == "" {
		return nil
	}
	targetARN := ""
	if isARN(certARN) {
		targetARN = certARN
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipVPCLatticeServiceUsesCertificate,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(service.ARN),
		TargetResourceID: certARN,
		TargetARN:        targetARN,
		TargetType:       awscloud.ResourceTypeACMCertificate,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipVPCLatticeServiceUsesCertificate + ":" + certARN,
	}
}

// targetGroupVPCRelationship records a target group's backing VPC. AWS reports
// the bare VPC id, which is the resource_id the EC2 scanner publishes. It
// returns nil when no VPC is reported (for example LAMBDA target groups).
func targetGroupVPCRelationship(
	boundary awscloud.Boundary,
	group TargetGroup,
) *awscloud.RelationshipObservation {
	vpcID := strings.TrimSpace(group.VPCID)
	if vpcID == "" {
		return nil
	}
	sourceID := targetGroupResourceID(group)
	if sourceID == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipVPCLatticeTargetGroupInVPC,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(group.ARN),
		TargetResourceID: vpcID,
		TargetType:       awscloud.ResourceTypeEC2VPC,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipVPCLatticeTargetGroupInVPC + ":" + vpcID,
	}
}

// targetGroupServiceRelationship records a target group's use by a service. AWS
// reports the service ARN, the resource_id the service node publishes. It
// returns nil when either endpoint identity is missing.
func targetGroupServiceRelationship(
	boundary awscloud.Boundary,
	group TargetGroup,
	serviceARN string,
) *awscloud.RelationshipObservation {
	sourceID := targetGroupResourceID(group)
	targetID := strings.TrimSpace(serviceARN)
	if sourceID == "" || targetID == "" {
		return nil
	}
	targetARNValue := ""
	if isARN(targetID) {
		targetARNValue = targetID
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipVPCLatticeTargetGroupServesService,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(group.ARN),
		TargetResourceID: targetID,
		TargetARN:        targetARNValue,
		TargetType:       awscloud.ResourceTypeVPCLatticeService,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipVPCLatticeTargetGroupServesService + ":" + targetID,
	}
}

// targetGroupTargetRelationship records a target group's registered target as a
// graph edge to the backing resource, keyed exactly how the owning scanner
// publishes the target's resource_id. It returns nil for IP targets and any
// target whose id does not resolve to the form the target group type implies,
// so the scanner never keys a dangling edge.
func targetGroupTargetRelationship(
	boundary awscloud.Boundary,
	group TargetGroup,
	target Target,
) *awscloud.RelationshipObservation {
	sourceID := targetGroupResourceID(group)
	targetID := strings.TrimSpace(target.ID)
	if sourceID == "" || targetID == "" {
		return nil
	}
	relationshipType, targetType, targetARN, ok := resolveTargetEdge(group.Type, targetID)
	if !ok {
		return nil
	}
	attributes := map[string]any{}
	if status := strings.TrimSpace(target.Status); status != "" {
		attributes["status"] = status
	}
	if target.Port != 0 {
		attributes["port"] = target.Port
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: relationshipType,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(group.ARN),
		TargetResourceID: targetID,
		TargetARN:        targetARN,
		TargetType:       targetType,
		Attributes:       attributesOrNil(attributes),
		SourceRecordID:   sourceID + "->" + relationshipType + ":" + targetID,
	}
}

// resolveTargetEdge maps a target group type and a registered target id to the
// relationship type, target_type, and (ARN-shaped only) target_arn the edge
// must carry, or ok=false when the target is an IP target or its id does not
// match the resolvable shape the type implies. Keeping the type/id shape check
// here guarantees the scanner only emits an edge that joins the published
// target node identity.
func resolveTargetEdge(groupType, targetID string) (relationshipType, targetType, targetARN string, ok bool) {
	switch strings.ToUpper(strings.TrimSpace(groupType)) {
	case "LAMBDA":
		if !isARN(targetID) {
			return "", "", "", false
		}
		return awscloud.RelationshipVPCLatticeTargetGroupTargetsLambda,
			awscloud.ResourceTypeLambdaFunction, targetID, true
	case "INSTANCE":
		if !isInstanceID(targetID) {
			return "", "", "", false
		}
		return awscloud.RelationshipVPCLatticeTargetGroupTargetsInstance,
			ec2InstanceTargetType, "", true
	case "ALB":
		if !isARN(targetID) {
			return "", "", "", false
		}
		return awscloud.RelationshipVPCLatticeTargetGroupTargetsLoadBalancer,
			awscloud.ResourceTypeELBv2LoadBalancer, targetID, true
	default:
		// IP target groups register raw IP addresses, which are not a scanned
		// resource family; skip rather than dangle the edge.
		return "", "", "", false
	}
}

// ec2InstanceTargetType is the target_type for the target-group-targets-instance
// edge. It mirrors the value the elb scanner uses and the relguard
// KnownTargetTypeAllowlist forward-reference entry "aws_ec2_instance"; no EC2
// instance resource scanner exists yet, so the edge is a documented forward
// reference keyed by the bare instance id.
const ec2InstanceTargetType = "aws_ec2_instance"

// attributesOrNil returns attributes when non-empty, or nil so the emitted
// payload omits an empty attribute map.
func attributesOrNil(attributes map[string]any) map[string]any {
	if len(attributes) == 0 {
		return nil
	}
	return attributes
}
