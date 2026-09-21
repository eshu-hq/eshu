// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package elasticbeanstalk

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Elastic Beanstalk option-setting namespaces and option names that name
// concrete AWS resources. These are stable, AWS-documented configuration option
// identifiers, not author-controlled values, so matching on them is safe.
const (
	optionNamespaceVPC          = "aws:ec2:vpc"
	optionNameVPCID             = "VPCId"
	optionNamespaceLaunchConfig = "aws:autoscaling:launchconfiguration"
	optionNameInstanceProfile   = "IamInstanceProfile"
	optionNamespaceEnvironment  = "aws:elasticbeanstalk:environment"
	optionNameServiceRole       = "ServiceRole"
)

// genericResourceTargetType keeps a load-balancer relationship honest when the
// reported identifier is not an ELBv2 ARN. DescribeEnvironmentResources reports
// a classic-ELB load balancer by bare name, which the ELBv2 scanner does not
// model, so the edge falls back to the generic resource type rather than
// asserting a wrong ELBv2 target.
const genericResourceTargetType = "aws_resource"

// elbv2ARNMarker is the substring present in an Elastic Load Balancing v2
// (ALB/NLB) ARN. The ELBv2 scanner keys its load balancer nodes by ARN, so only
// an identifier carrying this marker may claim the ELBv2 target type and a real
// target_arn.
const elbv2ARNMarker = ":elasticloadbalancing:"

// environmentRelationships derives every relationship Elastic Beanstalk reports
// for one environment: the parent application, the VPC/IAM joins from deployed
// option settings, the load-balancer/Auto-Scaling-group/launch-template joins
// from the environment resource description, and the running application
// version.
func environmentRelationships(
	boundary awscloud.Boundary,
	environment Environment,
	resources EnvironmentResources,
	settings []OptionSetting,
	applicationARNByName map[string]string,
	versionARNByKey map[string]string,
) []awscloud.RelationshipObservation {
	environmentARN := strings.TrimSpace(environment.ARN)
	sourceID := firstNonEmpty(environmentARN, strings.TrimSpace(environment.ID), strings.TrimSpace(environment.Name))
	if sourceID == "" {
		return nil
	}

	var observations []awscloud.RelationshipObservation
	if rel, ok := applicationRelationship(boundary, environment, environmentARN, sourceID, applicationARNByName); ok {
		observations = append(observations, rel)
	}
	observations = append(observations, optionSettingRelationships(boundary, environment, environmentARN, sourceID, settings)...)
	observations = append(observations, resourceRelationships(boundary, environmentARN, sourceID, resources)...)
	if rel, ok := versionRelationship(boundary, environment, environmentARN, sourceID, versionARNByKey); ok {
		observations = append(observations, rel)
	}
	return observations
}

func applicationRelationship(
	boundary awscloud.Boundary,
	environment Environment,
	environmentARN, sourceID string,
	applicationARNByName map[string]string,
) (awscloud.RelationshipObservation, bool) {
	appName := strings.TrimSpace(environment.ApplicationName)
	if appName == "" {
		return awscloud.RelationshipObservation{}, false
	}
	appARN := strings.TrimSpace(applicationARNByName[appName])
	// Join against the application node, whose resource_id is the application
	// ARN when known. Fall back to the bare name so the edge still carries a
	// stable target identity for downstream correlation.
	target := firstNonEmpty(appARN, appName)
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipElasticBeanstalkEnvironmentBelongsToApplication,
		SourceResourceID: sourceID,
		SourceARN:        environmentARN,
		TargetResourceID: target,
		TargetARN:        appARN,
		TargetType:       awscloud.ResourceTypeElasticBeanstalkApplication,
		SourceRecordID:   sourceID + "#application#" + appName,
	}, true
}

// optionSettingRelationships derives the VPC and IAM joins from the deployed
// option settings. Only the resource-identity options are read; option values
// such as environment variables are never turned into relationships.
func optionSettingRelationships(
	boundary awscloud.Boundary,
	environment Environment,
	environmentARN, sourceID string,
	settings []OptionSetting,
) []awscloud.RelationshipObservation {
	var observations []awscloud.RelationshipObservation
	for _, setting := range settings {
		namespace := strings.TrimSpace(setting.Namespace)
		option := strings.TrimSpace(setting.OptionName)
		value := strings.TrimSpace(setting.Value)
		if value == "" {
			continue
		}
		switch {
		case namespace == optionNamespaceVPC && option == optionNameVPCID:
			observations = append(observations, awscloud.RelationshipObservation{
				Boundary:         boundary,
				RelationshipType: awscloud.RelationshipElasticBeanstalkEnvironmentUsesVPC,
				SourceResourceID: sourceID,
				SourceARN:        environmentARN,
				TargetResourceID: value,
				TargetType:       awscloud.ResourceTypeEC2VPC,
				SourceRecordID:   sourceID + "#vpc#" + value,
			})
		case namespace == optionNamespaceLaunchConfig && option == optionNameInstanceProfile:
			observations = append(observations, awscloud.RelationshipObservation{
				Boundary:         boundary,
				RelationshipType: awscloud.RelationshipElasticBeanstalkEnvironmentUsesInstanceProfile,
				SourceResourceID: sourceID,
				SourceARN:        environmentARN,
				TargetResourceID: value,
				TargetARN:        arnOrEmpty(value),
				TargetType:       awscloud.ResourceTypeIAMInstanceProfile,
				SourceRecordID:   sourceID + "#instance-profile#" + value,
			})
		case namespace == optionNamespaceEnvironment && option == optionNameServiceRole:
			observations = append(observations, awscloud.RelationshipObservation{
				Boundary:         boundary,
				RelationshipType: awscloud.RelationshipElasticBeanstalkEnvironmentUsesServiceRole,
				SourceResourceID: sourceID,
				SourceARN:        environmentARN,
				TargetResourceID: value,
				TargetARN:        arnOrEmpty(value),
				TargetType:       awscloud.ResourceTypeIAMRole,
				SourceRecordID:   sourceID + "#service-role#" + value,
			})
		}
	}
	return observations
}

// resourceRelationships derives the load-balancer, Auto-Scaling-group, and
// launch-template joins from the environment resource description reported by
// DescribeEnvironmentResources.
func resourceRelationships(
	boundary awscloud.Boundary,
	environmentARN, sourceID string,
	resources EnvironmentResources,
) []awscloud.RelationshipObservation {
	var observations []awscloud.RelationshipObservation
	for _, identifier := range resources.LoadBalancerNames {
		identifier = strings.TrimSpace(identifier)
		if identifier == "" {
			continue
		}
		targetType, targetARN := loadBalancerTarget(identifier)
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipElasticBeanstalkEnvironmentUsesLoadBalancer,
			SourceResourceID: sourceID,
			SourceARN:        environmentARN,
			TargetResourceID: identifier,
			TargetARN:        targetARN,
			TargetType:       targetType,
			SourceRecordID:   sourceID + "#load-balancer#" + identifier,
		})
	}
	for _, name := range resources.AutoScalingGroupNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipElasticBeanstalkEnvironmentUsesAutoScalingGroup,
			SourceResourceID: sourceID,
			SourceARN:        environmentARN,
			TargetResourceID: name,
			TargetType:       awscloud.ResourceTypeAutoScalingGroup,
			SourceRecordID:   sourceID + "#asg#" + name,
		})
	}
	for _, id := range resources.LaunchTemplateIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipElasticBeanstalkEnvironmentUsesLaunchTemplate,
			SourceResourceID: sourceID,
			SourceARN:        environmentARN,
			TargetResourceID: id,
			TargetType:       awscloud.ResourceTypeEC2LaunchTemplate,
			SourceRecordID:   sourceID + "#launch-template#" + id,
		})
	}
	return observations
}

func versionRelationship(
	boundary awscloud.Boundary,
	environment Environment,
	environmentARN, sourceID string,
	versionARNByKey map[string]string,
) (awscloud.RelationshipObservation, bool) {
	label := strings.TrimSpace(environment.VersionLabel)
	if label == "" {
		return awscloud.RelationshipObservation{}, false
	}
	appName := strings.TrimSpace(environment.ApplicationName)
	versionARN := strings.TrimSpace(versionARNByKey[versionKey(appName, label)])
	// Join against the application-version node, whose resource_id is the
	// version ARN when known. Fall back to the application/version key so the
	// edge still carries a stable target identity.
	target := firstNonEmpty(versionARN, versionKey(appName, label))
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipElasticBeanstalkEnvironmentRunsVersion,
		SourceResourceID: sourceID,
		SourceARN:        environmentARN,
		TargetResourceID: target,
		TargetARN:        versionARN,
		TargetType:       awscloud.ResourceTypeElasticBeanstalkApplicationVersion,
		SourceRecordID:   sourceID + "#version#" + label,
	}, true
}

// loadBalancerTarget classifies a load-balancer identifier reported by
// DescribeEnvironmentResources into the Eshu target type it can actually join.
// Elastic Beanstalk reports an ELBv2 (ALB/NLB) load balancer by ARN and a
// Classic Load Balancer by bare name. The ELBv2 scanner keys its nodes by ARN,
// so an ELBv2 ARN target both keeps the ELBv2 type and carries a real
// target_arn for the join. A non-ARN identifier (a Classic ELB name, which has
// no ELBv2 node) falls back to the generic resource type and never fabricates
// an ARN, so the edge cannot mis-join an ELBv2 node it does not match.
func loadBalancerTarget(identifier string) (targetType, targetARN string) {
	id := strings.TrimSpace(identifier)
	if strings.HasPrefix(id, "arn:") && strings.Contains(id, elbv2ARNMarker) {
		return awscloud.ResourceTypeELBv2LoadBalancer, id
	}
	return genericResourceTargetType, ""
}

// arnOrEmpty returns the value only when it is already an ARN. Elastic
// Beanstalk option settings accept either a bare IAM name or a full ARN; the
// scanner records the raw value as the target id and only sets target_arn when
// AWS reported a real ARN, so it never fabricates an arn:aws: string.
func arnOrEmpty(value string) string {
	if strings.HasPrefix(strings.TrimSpace(value), "arn:") {
		return strings.TrimSpace(value)
	}
	return ""
}
