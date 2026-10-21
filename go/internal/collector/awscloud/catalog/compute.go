// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"

// Compute-category constants consolidated from constants_ec2.go (#6696).

const (
	// ServiceEC2 identifies the regional Amazon Elastic Compute Cloud network
	// topology scan slice.
	ServiceEC2 = "ec2"
)

const (
	// ResourceTypeEC2VPC identifies an EC2 VPC.
	ResourceTypeEC2VPC = awsv1.ResourceTypeEC2VPC
	// ResourceTypeEC2Subnet identifies an EC2 subnet.
	ResourceTypeEC2Subnet = awsv1.ResourceTypeEC2Subnet
	// ResourceTypeEC2SecurityGroup identifies an EC2 security group.
	ResourceTypeEC2SecurityGroup = awsv1.ResourceTypeEC2SecurityGroup
	// ResourceTypeEC2SecurityGroupRule identifies an EC2 security group rule.
	ResourceTypeEC2SecurityGroupRule = awsv1.ResourceTypeEC2SecurityGroupRule
	// ResourceTypeEC2NetworkInterface identifies an EC2 network interface.
	ResourceTypeEC2NetworkInterface = awsv1.ResourceTypeEC2NetworkInterface
	// ResourceTypeEC2Volume identifies an EBS volume observed through the EC2
	// DescribeVolumes API.
	ResourceTypeEC2Volume = awsv1.ResourceTypeEC2Volume
	// ResourceTypeEC2Instance identifies an EC2 instance. It is the join anchor
	// for the metadata-only ec2_instance_posture fact AND (#5448) the identity
	// aws_resource fact the EC2 scanner emits per instance carrying the AMI
	// (ImageId) the instance was launched from. Both facts resolve to the same
	// canonical cloud_resource_uid; the posture fact remains the sole owner of
	// the CloudResource node's base identity/posture properties, and the
	// identity fact only ever augments the node with the disjoint ami_id
	// property (go/internal/reducer/ec2instance/ec2_instance_identity_materialization.go).
	ResourceTypeEC2Instance = awsv1.ResourceTypeEC2Instance
	// ResourceTypeEC2AMI identifies an EC2 AMI (machine image), both as a
	// relationship target (#5448) and, since #5717, as the resource_type of
	// the AMI's own aws_resource identity fact (amiResourceObservation in
	// go/internal/collector/awscloud/service/ec2/ami_identity.go). The AMI
	// materializes as an ordinary CloudResource node — no dedicated node/edge
	// writer, no new graph label — through the SAME generic AWS resource
	// materialization and relationship-edge-join path every other resource
	// type uses (go/internal/reducer/aws_resource_materialization.go,
	// aws_relationship_join.go). It carries only identity: the EC2 scanner
	// reads no DescribeImages data, so the AMI carries only the bare
	// resource-id-as-Name (the EC2-wide convention) with no rich state,
	// owner, or creation-date metadata — a documented scope boundary, not
	// a bug (enrichment via DescribeImages is a separate, costed change).
	ResourceTypeEC2AMI = awsv1.ResourceTypeEC2AMI
)

const (
	// RelationshipEC2SubnetInVPC records subnet membership in a VPC.
	RelationshipEC2SubnetInVPC = "ec2_subnet_in_vpc"
	// RelationshipEC2SecurityGroupInVPC records security group membership in a
	// VPC.
	RelationshipEC2SecurityGroupInVPC = "ec2_security_group_in_vpc"
	// RelationshipEC2SecurityGroupHasRule records a security group child rule.
	RelationshipEC2SecurityGroupHasRule = "ec2_security_group_has_rule"
	// RelationshipEC2NetworkInterfaceInSubnet records ENI placement in a
	// subnet.
	RelationshipEC2NetworkInterfaceInSubnet = "ec2_network_interface_in_subnet"
	// RelationshipEC2NetworkInterfaceInVPC records ENI placement in a VPC.
	RelationshipEC2NetworkInterfaceInVPC = "ec2_network_interface_in_vpc"
	// RelationshipEC2NetworkInterfaceUsesSecurityGroup records security group
	// attachment to an ENI.
	RelationshipEC2NetworkInterfaceUsesSecurityGroup = "ec2_network_interface_uses_security_group"
	// RelationshipEC2NetworkInterfaceAttachedToResource records ENI attachment
	// evidence without emitting the attached resource as an inventory fact.
	RelationshipEC2NetworkInterfaceAttachedToResource = "ec2_network_interface_attached_to_resource"
	// RelationshipEC2VolumeUsesKMSKey records the KMS key AWS reports for EBS
	// volume encryption.
	RelationshipEC2VolumeUsesKMSKey = "ec2_volume_uses_kms_key"
	// RelationshipEC2InstanceUsesAMI records the AMI (ImageId) an EC2 instance
	// was launched from (#5448). The target type is ResourceTypeEC2AMI, which
	// the AMI's own aws_resource identity fact (#5717) now materializes as a
	// CloudResource node, so this relationship's edge projection resolves
	// (see ResourceTypeEC2AMI doc).
	RelationshipEC2InstanceUsesAMI = "ec2_instance_uses_ami"
)

// Compute-category constants consolidated from constants_autoscaling.go (#6696).

const (
	// ServiceAutoScaling identifies the regional Amazon EC2 Auto Scaling
	// metadata scan slice.
	ServiceAutoScaling = "autoscaling"
)

const (
	// ResourceTypeAutoScalingLaunchConfiguration identifies an EC2 Auto Scaling
	// launch configuration. Only the name and ARN are emitted; the launch
	// configuration UserData (which can hold bootstrap secrets) is never read or
	// persisted.
	ResourceTypeAutoScalingLaunchConfiguration = "aws_autoscaling_launch_configuration"
	// ResourceTypeAutoScalingPolicy identifies an EC2 Auto Scaling scaling
	// policy.
	ResourceTypeAutoScalingPolicy = "aws_autoscaling_policy"
	// ResourceTypeAutoScalingLifecycleHook identifies an EC2 Auto Scaling
	// lifecycle hook.
	ResourceTypeAutoScalingLifecycleHook = "aws_autoscaling_lifecycle_hook"
	// ResourceTypeAutoScalingScheduledAction identifies an EC2 Auto Scaling
	// scheduled action.
	ResourceTypeAutoScalingScheduledAction = "aws_autoscaling_scheduled_action"
)

const (
	// RelationshipAutoScalingGroupUsesLaunchTemplate records the EC2 launch
	// template an Auto Scaling group launches instances from. The edge keys on
	// the launch template ID (lt-...) when reported, otherwise the launch
	// template name, matching the EC2 launch-template resource_id form owned by
	// the Batch scanner.
	RelationshipAutoScalingGroupUsesLaunchTemplate = "autoscaling_group_uses_launch_template"
	// RelationshipAutoScalingGroupUsesLaunchConfiguration records the launch
	// configuration an Auto Scaling group launches instances from. The edge keys
	// on the launch configuration name.
	RelationshipAutoScalingGroupUsesLaunchConfiguration = "autoscaling_group_uses_launch_configuration"
	// RelationshipAutoScalingGroupUsesSubnet records a subnet placement for an
	// Auto Scaling group. The edge keys on the bare subnet ID, matching the
	// EC2-owned subnet resource_id form.
	RelationshipAutoScalingGroupUsesSubnet = "autoscaling_group_uses_subnet"
	// RelationshipAutoScalingGroupAttachedToTargetGroup records an ELBv2 target
	// group an Auto Scaling group registers instances with. The edge keys on the
	// target group ARN, matching the ELBv2-owned target-group resource_id form.
	RelationshipAutoScalingGroupAttachedToTargetGroup = "autoscaling_group_attached_to_target_group"
	// RelationshipAutoScalingGroupUsesIAMRole records the service-linked IAM
	// role an Auto Scaling group assumes to call other AWS services. The edge
	// keys on the role ARN.
	RelationshipAutoScalingGroupUsesIAMRole = "autoscaling_group_uses_iam_role"
	// RelationshipAutoScalingPolicyTargetsGroup records the Auto Scaling group a
	// scaling policy applies to.
	RelationshipAutoScalingPolicyTargetsGroup = "autoscaling_policy_targets_group"
	// RelationshipAutoScalingLifecycleHookTargetsGroup records the Auto Scaling
	// group a lifecycle hook is defined on.
	RelationshipAutoScalingLifecycleHookTargetsGroup = "autoscaling_lifecycle_hook_targets_group"
	// RelationshipAutoScalingScheduledActionTargetsGroup records the Auto
	// Scaling group a scheduled action is defined on.
	RelationshipAutoScalingScheduledActionTargetsGroup = "autoscaling_scheduled_action_targets_group"
)

// Compute-category constants consolidated from constants_lambda.go (#6696).

const (
	// ServiceLambda identifies the regional AWS Lambda service scan slice.
	ServiceLambda = "lambda"
)

const (
	// ResourceTypeLambdaFunction identifies a Lambda function.
	ResourceTypeLambdaFunction = "aws_lambda_function"
	// ResourceTypeLambdaAlias identifies a Lambda alias.
	ResourceTypeLambdaAlias = "aws_lambda_alias"
	// ResourceTypeLambdaEventSourceMapping identifies a Lambda event source
	// mapping.
	ResourceTypeLambdaEventSourceMapping = "aws_lambda_event_source_mapping"
)

const (
	// RelationshipLambdaAliasTargetsFunction records alias routing to a Lambda
	// function version.
	RelationshipLambdaAliasTargetsFunction = "lambda_alias_targets_function"
	// RelationshipLambdaEventSourceMappingTargetsFunction records an event
	// source mapping target function.
	RelationshipLambdaEventSourceMappingTargetsFunction = "lambda_event_source_mapping_targets_function"
	// RelationshipLambdaFunctionUsesImage records a Lambda container image URI.
	RelationshipLambdaFunctionUsesImage = "lambda_function_uses_image"
	// RelationshipLambdaFunctionUsesExecutionRole records a Lambda execution
	// role.
	RelationshipLambdaFunctionUsesExecutionRole = "lambda_function_uses_execution_role"
	// RelationshipLambdaFunctionUsesSubnet records Lambda VPC subnet placement.
	RelationshipLambdaFunctionUsesSubnet = "lambda_function_uses_subnet"
	// RelationshipLambdaFunctionUsesSecurityGroup records Lambda VPC security
	// group attachment.
	RelationshipLambdaFunctionUsesSecurityGroup = "lambda_function_uses_security_group"
)

// Compute-category constants consolidated from constants_ecs.go (#6696).

const (
	// ServiceECS identifies the regional Amazon Elastic Container Service
	// service scan slice.
	ServiceECS = "ecs"
)

const (
	// ResourceTypeECSCluster identifies an ECS cluster.
	ResourceTypeECSCluster = "aws_ecs_cluster"
	// ResourceTypeECSService identifies an ECS service.
	ResourceTypeECSService = "aws_ecs_service"
	// ResourceTypeECSTaskDefinition identifies an ECS task definition.
	ResourceTypeECSTaskDefinition = "aws_ecs_task_definition"
	// ResourceTypeECSTask identifies an ECS task.
	ResourceTypeECSTask = "aws_ecs_task"
)

const (
	// RelationshipECSServiceUsesTaskDefinition records the task definition a
	// service currently runs.
	RelationshipECSServiceUsesTaskDefinition = "ecs_service_uses_task_definition"
	// RelationshipECSTaskDefinitionUsesImage records a container image
	// referenced by a task definition.
	RelationshipECSTaskDefinitionUsesImage = "ecs_task_definition_uses_image"
	// RelationshipECSServiceTargetsLoadBalancer records an ECS service load
	// balancer or target group binding.
	RelationshipECSServiceTargetsLoadBalancer = "ecs_service_targets_load_balancer"
	// RelationshipECSTaskUsesNetworkInterface records an ECS task ENI
	// attachment reported by DescribeTasks.
	RelationshipECSTaskUsesNetworkInterface = "ecs_task_uses_network_interface"
)

// Compute-category constants consolidated from constants_eks.go (#6696).

const (
	// ServiceEKS identifies the regional Amazon Elastic Kubernetes Service scan
	// slice.
	ServiceEKS = "eks"
)

const (
	// ResourceTypeEKSCluster identifies an EKS cluster.
	ResourceTypeEKSCluster = "aws_eks_cluster"
	// ResourceTypeEKSNodegroup identifies an EKS managed node group.
	ResourceTypeEKSNodegroup = "aws_eks_nodegroup"
	// ResourceTypeEKSAddon identifies an EKS managed add-on.
	ResourceTypeEKSAddon = "aws_eks_addon"
	// ResourceTypeEKSOIDCProvider identifies OIDC provider evidence associated
	// with an EKS cluster.
	ResourceTypeEKSOIDCProvider = "aws_eks_oidc_provider"
)

const (
	// RelationshipEKSClusterUsesIAMRole records an EKS cluster service role.
	RelationshipEKSClusterUsesIAMRole = "eks_cluster_uses_iam_role"
	// RelationshipEKSClusterUsesSubnet records EKS cluster subnet placement.
	RelationshipEKSClusterUsesSubnet = "eks_cluster_uses_subnet"
	// RelationshipEKSClusterUsesSecurityGroup records EKS cluster security group
	// placement.
	RelationshipEKSClusterUsesSecurityGroup = "eks_cluster_uses_security_group"
	// RelationshipEKSClusterHasOIDCProvider records an EKS cluster's OIDC
	// provider evidence for IRSA trust.
	RelationshipEKSClusterHasOIDCProvider = "eks_cluster_has_oidc_provider"
	// RelationshipEKSClusterHasNodegroup records managed node group membership
	// on an EKS cluster.
	RelationshipEKSClusterHasNodegroup = "eks_cluster_has_nodegroup"
	// RelationshipEKSClusterHasAddon records managed add-on membership on an EKS
	// cluster.
	RelationshipEKSClusterHasAddon = "eks_cluster_has_addon"
	// RelationshipEKSNodegroupUsesIAMRole records the IAM role used by an EKS
	// managed node group.
	RelationshipEKSNodegroupUsesIAMRole = "eks_nodegroup_uses_iam_role"
	// RelationshipEKSNodegroupUsesSubnet records an EKS managed node group
	// subnet.
	RelationshipEKSNodegroupUsesSubnet = "eks_nodegroup_uses_subnet"
	// RelationshipEKSAddonUsesIAMRole records the IAM role used by an EKS
	// managed add-on.
	RelationshipEKSAddonUsesIAMRole = "eks_addon_uses_iam_role"
)

// Compute-category constants consolidated from constants_ecr.go (#6696).

const (
	// ServiceECR identifies the regional Amazon Elastic Container Registry
	// service scan slice.
	ServiceECR = "ecr"
)

const (
	// ResourceTypeECRRepository identifies an ECR repository.
	ResourceTypeECRRepository = "aws_ecr_repository"
	// ResourceTypeECRLifecyclePolicy identifies an ECR repository lifecycle
	// policy child resource.
	ResourceTypeECRLifecyclePolicy = "aws_ecr_lifecycle_policy"
)

// Compute-category constants consolidated from constants_batch.go (#6696).

const (
	// ServiceBatch identifies the regional AWS Batch service scan slice.
	ServiceBatch = "batch"
)

const (
	// ResourceTypeBatchComputeEnvironment identifies an AWS Batch compute
	// environment.
	ResourceTypeBatchComputeEnvironment = "aws_batch_compute_environment"
	// ResourceTypeBatchJobQueue identifies an AWS Batch job queue.
	ResourceTypeBatchJobQueue = "aws_batch_job_queue"
	// ResourceTypeBatchJobDefinition identifies an AWS Batch job definition.
	ResourceTypeBatchJobDefinition = "aws_batch_job_definition"
	// ResourceTypeBatchSchedulingPolicy identifies an AWS Batch fair-share
	// scheduling policy. Only the policy name and ARN are emitted; the
	// fair-share weight state is never persisted.
	ResourceTypeBatchSchedulingPolicy = "aws_batch_scheduling_policy"
	// ResourceTypeBatchJob identifies a single AWS Batch job observed in a
	// recent-jobs listing. Only identity, status, and job-definition reference
	// are emitted; job parameters and container overrides are never persisted.
	ResourceTypeBatchJob = "aws_batch_job"
	// ResourceTypeEC2LaunchTemplate identifies an EC2 launch template referenced
	// by a Batch compute environment. Batch owns this target-type label because
	// no launch-template inventory scanner exists yet; the resource_id is the
	// launch template ID (lt-...) when reported, otherwise the launch template
	// name.
	ResourceTypeEC2LaunchTemplate = "aws_ec2_launch_template"
)

const (
	// RelationshipBatchJobQueueUsesComputeEnvironment records that a job queue
	// dispatches to a compute environment in its compute-environment order.
	RelationshipBatchJobQueueUsesComputeEnvironment = "batch_job_queue_uses_compute_environment"
	// RelationshipBatchComputeEnvironmentUsesSubnet records a subnet placement
	// for a compute environment. The compute-environment-to-VPC join is reached
	// transitively through this edge plus the EC2-owned subnet-to-VPC edge,
	// because the Batch API reports compute-resource subnets but no VPC ID.
	RelationshipBatchComputeEnvironmentUsesSubnet = "batch_compute_environment_uses_subnet"
	// RelationshipBatchComputeEnvironmentUsesSecurityGroup records a security
	// group attached to a compute environment's compute resources, providing the
	// EC2 network-fabric join alongside the subnet edge.
	RelationshipBatchComputeEnvironmentUsesSecurityGroup = "batch_compute_environment_uses_security_group"
	// RelationshipBatchComputeEnvironmentUsesLaunchTemplate records an EC2 or
	// Fargate launch template referenced by a compute environment.
	RelationshipBatchComputeEnvironmentUsesLaunchTemplate = "batch_compute_environment_uses_launch_template"
	// RelationshipBatchComputeEnvironmentUsesIAMRole records the Batch service
	// role or the EC2 instance-profile role used by a compute environment.
	RelationshipBatchComputeEnvironmentUsesIAMRole = "batch_compute_environment_uses_iam_role"
	// RelationshipBatchJobDefinitionUsesIAMRole records the job or execution IAM
	// role declared by a job definition's container properties.
	RelationshipBatchJobDefinitionUsesIAMRole = "batch_job_definition_uses_iam_role"
	// RelationshipBatchJobDefinitionUsesImage records a container image URI
	// referenced by a job definition's container properties.
	RelationshipBatchJobDefinitionUsesImage = "batch_job_definition_uses_image"
	// RelationshipBatchJobDefinitionReferencesSecret records a Secrets Manager
	// or SSM Parameter Store ARN referenced by a job definition container. The
	// reference (value_from ARN) is recorded as an edge; the resolved secret
	// value is never read or persisted.
	RelationshipBatchJobDefinitionReferencesSecret = "batch_job_definition_references_secret"
)

// Compute-category constants consolidated from constants_lightsail.go (#6696).

const (
	// ServiceLightsail identifies the regional Amazon Lightsail metadata-only
	// scan slice covering instances, managed relational databases, load
	// balancers, block-storage disks, and static IPs.
	ServiceLightsail = "lightsail"
)

const (
	// ResourceTypeLightsailInstance identifies an Amazon Lightsail virtual
	// private server (instance) metadata resource.
	ResourceTypeLightsailInstance = "aws_lightsail_instance"
	// ResourceTypeLightsailDatabase identifies an Amazon Lightsail managed
	// relational database metadata resource.
	ResourceTypeLightsailDatabase = "aws_lightsail_database"
	// ResourceTypeLightsailLoadBalancer identifies an Amazon Lightsail load
	// balancer metadata resource.
	ResourceTypeLightsailLoadBalancer = "aws_lightsail_load_balancer"
	// ResourceTypeLightsailDisk identifies an Amazon Lightsail block-storage
	// disk metadata resource.
	ResourceTypeLightsailDisk = "aws_lightsail_disk"
	// ResourceTypeLightsailStaticIP identifies an Amazon Lightsail static IP
	// metadata resource.
	ResourceTypeLightsailStaticIP = "aws_lightsail_static_ip"
)

const (
	// RelationshipLightsailLoadBalancerTargetsInstance records a Lightsail load
	// balancer's reported attachment to a Lightsail instance. The edge keys the
	// target on the bare Lightsail instance name, matching the instance node's
	// resource_id.
	RelationshipLightsailLoadBalancerTargetsInstance = "lightsail_load_balancer_targets_instance"
	// RelationshipLightsailInstanceAttachedToDisk records a Lightsail instance's
	// reported attachment to a Lightsail block-storage disk. The edge keys the
	// target on the bare Lightsail disk name, matching the disk node's
	// resource_id.
	RelationshipLightsailInstanceAttachedToDisk = "lightsail_instance_attached_to_disk"
	// RelationshipLightsailInstanceAttachedToStaticIP records a Lightsail
	// instance's reported attachment to a Lightsail static IP. The edge keys the
	// target on the bare Lightsail static IP name, matching the static IP node's
	// resource_id.
	RelationshipLightsailInstanceAttachedToStaticIP = "lightsail_instance_attached_to_static_ip"
)
