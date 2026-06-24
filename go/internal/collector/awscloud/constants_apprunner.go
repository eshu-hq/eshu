// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceAppRunner identifies the regional AWS App Runner service scan
	// slice.
	ServiceAppRunner = "apprunner"
)

const (
	// ResourceTypeAppRunnerService identifies an App Runner service. The
	// resource_id is the service ARN, matching the in-use-by / associated-
	// resource ARN that the ACM and WAFv2 scanners emit as their App Runner
	// edge target so those dangling edges resolve to this resource.
	ResourceTypeAppRunnerService = "aws_apprunner_service"
	// ResourceTypeAppRunnerConnection identifies an App Runner connection used
	// to reach a source code repository provider (for example GitHub or
	// Bitbucket). The resource_id is the connection ARN.
	ResourceTypeAppRunnerConnection = "aws_apprunner_connection"
	// ResourceTypeAppRunnerAutoScalingConfiguration identifies an App Runner
	// automatic scaling configuration revision. The resource_id is the
	// configuration ARN.
	ResourceTypeAppRunnerAutoScalingConfiguration = "aws_apprunner_autoscaling_configuration"
	// ResourceTypeAppRunnerObservabilityConfiguration identifies an App Runner
	// observability configuration revision. The resource_id is the
	// configuration ARN.
	ResourceTypeAppRunnerObservabilityConfiguration = "aws_apprunner_observability_configuration"
	// ResourceTypeAppRunnerVpcConnector identifies an App Runner VPC connector.
	// The resource_id is the VPC connector ARN.
	ResourceTypeAppRunnerVpcConnector = "aws_apprunner_vpc_connector"
	// ResourceTypeAppRunnerVpcIngressConnection identifies an App Runner VPC
	// ingress connection. The resource_id is the VPC ingress connection ARN.
	ResourceTypeAppRunnerVpcIngressConnection = "aws_apprunner_vpc_ingress_connection"
)

const (
	// RelationshipAppRunnerServiceUsesImage records a container image URI a
	// service deploys from a source image repository. The target keys on the
	// container image identifier (container_image), matching the ECS and Batch
	// container-image join.
	RelationshipAppRunnerServiceUsesImage = "apprunner_service_uses_image"
	// RelationshipAppRunnerServiceUsesConnection records the App Runner
	// connection a source-code service uses to reach its repository provider.
	RelationshipAppRunnerServiceUsesConnection = "apprunner_service_uses_connection"
	// RelationshipAppRunnerServiceUsesIAMRole records the instance role or the
	// ECR access role a service uses. The target keys on the IAM role ARN.
	RelationshipAppRunnerServiceUsesIAMRole = "apprunner_service_uses_iam_role"
	// RelationshipAppRunnerServiceUsesKMSKey records the customer-managed KMS
	// key a service uses to encrypt logs and the source-repository copy.
	RelationshipAppRunnerServiceUsesKMSKey = "apprunner_service_uses_kms_key"
	// RelationshipAppRunnerServiceUsesVpcConnector records the VPC connector a
	// service uses for outbound (egress) VPC traffic.
	RelationshipAppRunnerServiceUsesVpcConnector = "apprunner_service_uses_vpc_connector"
	// RelationshipAppRunnerServiceUsesAutoScalingConfiguration records the
	// automatic scaling configuration revision associated with a service.
	RelationshipAppRunnerServiceUsesAutoScalingConfiguration = "apprunner_service_uses_autoscaling_configuration"
	// RelationshipAppRunnerServiceUsesObservabilityConfiguration records the
	// observability configuration revision associated with a service.
	RelationshipAppRunnerServiceUsesObservabilityConfiguration = "apprunner_service_uses_observability_configuration"
	// RelationshipAppRunnerServiceReferencesSecret records a Secrets Manager or
	// SSM Parameter Store ARN referenced by a service's runtime environment
	// secrets. Only the reference ARN is recorded; the resolved value is never
	// read or persisted.
	RelationshipAppRunnerServiceReferencesSecret = "apprunner_service_references_secret"
	// RelationshipAppRunnerVpcConnectorUsesSubnet records a subnet a VPC
	// connector places network interfaces in. The target keys on the bare
	// subnet ID.
	RelationshipAppRunnerVpcConnectorUsesSubnet = "apprunner_vpc_connector_uses_subnet"
	// RelationshipAppRunnerVpcConnectorUsesSecurityGroup records a security
	// group a VPC connector attaches. The target keys on the bare security
	// group ID.
	RelationshipAppRunnerVpcConnectorUsesSecurityGroup = "apprunner_vpc_connector_uses_security_group"
	// RelationshipAppRunnerVpcIngressConnectionTargetsService records the
	// service a VPC ingress connection routes inbound traffic to.
	RelationshipAppRunnerVpcIngressConnectionTargetsService = "apprunner_vpc_ingress_connection_targets_service"
)
