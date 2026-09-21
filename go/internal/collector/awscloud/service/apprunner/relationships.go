// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package apprunner

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

const containerImageTargetType = "container_image"

// serviceRelationships records the image, connection, IAM-role, KMS-key,
// VPC-connector, autoscaling, observability, and secret-reference joins of one
// App Runner service. Every edge sets a non-empty target_type matching the
// target scanner's resource_id form, and the source resource_id is the service
// ARN so it matches the ACM/WAFv2 edge target join key.
func serviceRelationships(boundary awscloud.Boundary, service Service) []awscloud.RelationshipObservation {
	serviceARN := strings.TrimSpace(service.ARN)
	if serviceARN == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation

	add := func(relationshipType, targetID, targetType, recordSuffix string) {
		targetID = strings.TrimSpace(targetID)
		if targetID == "" {
			return
		}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: relationshipType,
			SourceResourceID: serviceARN,
			SourceARN:        serviceARN,
			TargetResourceID: targetID,
			TargetARN:        arnOrEmpty(targetID),
			TargetType:       targetType,
			SourceRecordID:   serviceARN + "#" + recordSuffix + "#" + targetID,
		})
	}

	if image := strings.TrimSpace(service.ImageIdentifier); image != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAppRunnerServiceUsesImage,
			SourceResourceID: serviceARN,
			SourceARN:        serviceARN,
			TargetResourceID: image,
			TargetType:       containerImageTargetType,
			SourceRecordID:   serviceARN + "#container-image#" + image,
		})
	}

	add(awscloud.RelationshipAppRunnerServiceUsesConnection, service.ConnectionARN, awscloud.ResourceTypeAppRunnerConnection, "connection")
	add(awscloud.RelationshipAppRunnerServiceUsesIAMRole, service.AccessRoleARN, awscloud.ResourceTypeIAMRole, "access-role")
	add(awscloud.RelationshipAppRunnerServiceUsesIAMRole, service.InstanceRoleARN, awscloud.ResourceTypeIAMRole, "instance-role")
	add(awscloud.RelationshipAppRunnerServiceUsesKMSKey, service.KMSKey, awscloud.ResourceTypeKMSKey, "kms-key")
	add(awscloud.RelationshipAppRunnerServiceUsesVpcConnector, service.VpcConnectorARN, awscloud.ResourceTypeAppRunnerVpcConnector, "vpc-connector")
	add(awscloud.RelationshipAppRunnerServiceUsesAutoScalingConfiguration, service.AutoScalingConfigurationARN, awscloud.ResourceTypeAppRunnerAutoScalingConfiguration, "autoscaling")
	add(awscloud.RelationshipAppRunnerServiceUsesObservabilityConfiguration, service.ObservabilityConfigurationARN, awscloud.ResourceTypeAppRunnerObservabilityConfiguration, "observability")

	for _, secret := range service.SecretReferences {
		valueFrom := strings.TrimSpace(secret.ValueFrom)
		if valueFrom == "" {
			continue
		}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAppRunnerServiceReferencesSecret,
			SourceResourceID: serviceARN,
			SourceARN:        serviceARN,
			TargetResourceID: valueFrom,
			TargetARN:        arnOrEmpty(valueFrom),
			TargetType:       secretReferenceTargetType(valueFrom),
			Attributes:       map[string]any{"name": strings.TrimSpace(secret.Name)},
			SourceRecordID:   serviceARN + "#secret#" + valueFrom,
		})
	}

	return observations
}

// vpcConnectorRelationships records the subnet and security-group joins of one
// App Runner VPC connector. Subnets and security groups key on the bare AWS IDs
// so the EC2-owned subnet/security-group resources resolve.
func vpcConnectorRelationships(boundary awscloud.Boundary, connector VpcConnector) []awscloud.RelationshipObservation {
	connectorARN := strings.TrimSpace(connector.ARN)
	if connectorARN == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	for _, subnetID := range dedupeStrings(connector.Subnets) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAppRunnerVpcConnectorUsesSubnet,
			SourceResourceID: connectorARN,
			SourceARN:        connectorARN,
			TargetResourceID: subnetID,
			TargetType:       awscloud.ResourceTypeEC2Subnet,
			SourceRecordID:   connectorARN + "#subnet#" + subnetID,
		})
	}
	for _, securityGroupID := range dedupeStrings(connector.SecurityGroups) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAppRunnerVpcConnectorUsesSecurityGroup,
			SourceResourceID: connectorARN,
			SourceARN:        connectorARN,
			TargetResourceID: securityGroupID,
			TargetType:       awscloud.ResourceTypeEC2SecurityGroup,
			SourceRecordID:   connectorARN + "#security-group#" + securityGroupID,
		})
	}
	return observations
}

// vpcIngressConnectionRelationships records the service a VPC ingress
// connection routes inbound traffic to. The target keys on the service ARN.
func vpcIngressConnectionRelationships(
	boundary awscloud.Boundary,
	ingress VpcIngressConnection,
) []awscloud.RelationshipObservation {
	ingressARN := strings.TrimSpace(ingress.ARN)
	serviceARN := strings.TrimSpace(ingress.ServiceARN)
	if ingressARN == "" || serviceARN == "" {
		return nil
	}
	return []awscloud.RelationshipObservation{{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipAppRunnerVpcIngressConnectionTargetsService,
		SourceResourceID: ingressARN,
		SourceARN:        ingressARN,
		TargetResourceID: serviceARN,
		TargetARN:        serviceARN,
		TargetType:       awscloud.ResourceTypeAppRunnerService,
		SourceRecordID:   ingressARN + "#service#" + serviceARN,
	}}
}

// secretReferenceTargetType classifies a runtime secret reference ARN as an SSM
// parameter when the ARN names the ssm service, otherwise as a Secrets Manager
// secret.
func secretReferenceTargetType(valueFrom string) string {
	if strings.HasPrefix(strings.TrimSpace(valueFrom), "arn:") && strings.Contains(valueFrom, ":ssm:") {
		return awscloud.ResourceTypeSSMParameter
	}
	return awscloud.ResourceTypeSecretsManagerSecret
}

// arnOrEmpty returns the candidate when it is an ARN, so relationship target
// ARNs stay accurate for non-ARN identities.
func arnOrEmpty(candidate string) string {
	candidate = strings.TrimSpace(candidate)
	if strings.HasPrefix(candidate, "arn:") {
		return candidate
	}
	return ""
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	output := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		output = append(output, value)
	}
	return output
}
