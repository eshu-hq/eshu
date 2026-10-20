// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package elb

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// loadBalancerARN synthesizes the partition-aware ARN for a Classic load
// balancer. Classic ELBs carry no AWS-assigned ARN, so the scanner builds one
// from the scan boundary and the load balancer name. The partition is derived
// from the boundary region so GovCloud and China joins resolve instead of
// dangling; the commercial partition is never hardcoded.
func loadBalancerARN(boundary awscloud.Boundary, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	region := strings.TrimSpace(boundary.Region)
	account := strings.TrimSpace(boundary.AccountID)
	return "arn:" + partition(boundary) + ":elasticloadbalancing:" + region + ":" + account + ":loadbalancer/" + name
}

// loadBalancerRelationships builds every graph-join edge for one Classic load
// balancer: registered instances, subnets, security groups, the VPC, and
// HTTPS/SSL listener certificates. Edges with no resolvable target are skipped.
func loadBalancerRelationships(
	boundary awscloud.Boundary,
	loadBalancer LoadBalancer,
	loadBalancerARN string,
) []awscloud.RelationshipObservation {
	if loadBalancerARN == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation

	for _, instanceID := range dedupe(loadBalancer.InstanceIDs) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipELBLoadBalancerRegistersInstance,
			SourceResourceID: loadBalancerARN,
			SourceARN:        loadBalancerARN,
			TargetResourceID: instanceID,
			TargetType:       ec2InstanceTargetType,
			SourceRecordID:   loadBalancerARN + "#instance#" + instanceID,
		})
	}

	for _, subnetID := range dedupe(loadBalancer.Subnets) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipELBLoadBalancerInSubnet,
			SourceResourceID: loadBalancerARN,
			SourceARN:        loadBalancerARN,
			TargetResourceID: subnetID,
			TargetType:       awscloud.ResourceTypeEC2Subnet,
			SourceRecordID:   loadBalancerARN + "#subnet#" + subnetID,
		})
	}

	for _, groupID := range dedupe(loadBalancer.SecurityGroups) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipELBLoadBalancerUsesSecurityGroup,
			SourceResourceID: loadBalancerARN,
			SourceARN:        loadBalancerARN,
			TargetResourceID: groupID,
			TargetType:       awscloud.ResourceTypeEC2SecurityGroup,
			SourceRecordID:   loadBalancerARN + "#security-group#" + groupID,
		})
	}

	if vpcID := strings.TrimSpace(loadBalancer.VPCID); vpcID != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipELBLoadBalancerInVPC,
			SourceResourceID: loadBalancerARN,
			SourceARN:        loadBalancerARN,
			TargetResourceID: vpcID,
			TargetType:       awscloud.ResourceTypeEC2VPC,
			SourceRecordID:   loadBalancerARN + "#vpc#" + vpcID,
		})
	}

	observations = append(observations, certificateRelationships(boundary, loadBalancer, loadBalancerARN)...)
	return observations
}

// certificateRelationships builds one edge per distinct HTTPS/SSL listener
// certificate ARN. The target type is chosen from the ARN service segment:
// an :acm: ARN targets aws_acm_certificate (a scanned resource), and an :iam:
// server-certificate ARN targets aws_iam_server_certificate (a documented
// forward reference). A listener with no certificate, or a certificate id that
// is not ARN-shaped, is skipped so no edge dangles.
func certificateRelationships(
	boundary awscloud.Boundary,
	loadBalancer LoadBalancer,
	loadBalancerARN string,
) []awscloud.RelationshipObservation {
	var observations []awscloud.RelationshipObservation
	seen := make(map[string]struct{})
	for _, listener := range loadBalancer.Listeners {
		certificateARN := strings.TrimSpace(listener.SSLCertificateID)
		if certificateARN == "" || !isARN(certificateARN) {
			continue
		}
		if _, ok := seen[certificateARN]; ok {
			continue
		}
		seen[certificateARN] = struct{}{}

		relationshipType, targetType, ok := certificateEdgeKind(certificateARN)
		if !ok {
			continue
		}
		// The edge is deduped by certificate ARN, but one certificate can back
		// several HTTPS/SSL listeners on different ports. Per-listener port and
		// protocol are therefore omitted here — they would reflect only the
		// first listener and silently misrepresent the rest. The full listener
		// list (ports/protocols) is preserved on the load balancer resource.
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: relationshipType,
			SourceResourceID: loadBalancerARN,
			SourceARN:        loadBalancerARN,
			TargetResourceID: certificateARN,
			TargetARN:        certificateARN,
			TargetType:       targetType,
			SourceRecordID:   loadBalancerARN + "#certificate#" + certificateARN,
		})
	}
	return observations
}

// certificateEdgeKind classifies a server certificate ARN as an ACM certificate
// or an IAM server certificate by its service segment. It returns ok=false for
// any other ARN service so the scanner does not key a dangling or mis-typed
// edge.
func certificateEdgeKind(certificateARN string) (relationshipType, targetType string, ok bool) {
	switch {
	case arnService(certificateARN) == "acm":
		return awscloud.RelationshipELBLoadBalancerUsesACMCertificate, awscloud.ResourceTypeACMCertificate, true
	case arnService(certificateARN) == "iam":
		return awscloud.RelationshipELBLoadBalancerUsesIAMServerCertificate, iamServerCertificateTargetType, true
	default:
		return "", "", false
	}
}

// partition returns the AWS partition for the scan boundary's region — aws,
// aws-cn, or aws-us-gov. Classic ELBs carry no ARN, so the boundary region is
// the partition source for the synthesized load balancer ARN; hardcoding the
// commercial partition would mis-key the resource node and every edge in
// GovCloud and China.
func partition(boundary awscloud.Boundary) string {
	region := strings.TrimSpace(boundary.Region)
	switch {
	case strings.HasPrefix(region, "us-gov-"):
		return "aws-us-gov"
	case strings.HasPrefix(region, "cn-"):
		return "aws-cn"
	default:
		return "aws"
	}
}
