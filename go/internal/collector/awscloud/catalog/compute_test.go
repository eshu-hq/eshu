// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import "testing"

// TestComputeConstantsMatchAWSCloudValues pins every compute-category constant
// to the exact value it carried in package awscloud before the #6696 catalog
// consolidation. The consolidation moves declarations only: any value drift
// here is a behavior change, not a move.
func TestComputeConstantsMatchAWSCloudValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "RelationshipAutoScalingGroupAttachedToTargetGroup", got: RelationshipAutoScalingGroupAttachedToTargetGroup, want: "autoscaling_group_attached_to_target_group"},
		{name: "RelationshipAutoScalingGroupUsesIAMRole", got: RelationshipAutoScalingGroupUsesIAMRole, want: "autoscaling_group_uses_iam_role"},
		{name: "RelationshipAutoScalingGroupUsesLaunchConfiguration", got: RelationshipAutoScalingGroupUsesLaunchConfiguration, want: "autoscaling_group_uses_launch_configuration"},
		{name: "RelationshipAutoScalingGroupUsesLaunchTemplate", got: RelationshipAutoScalingGroupUsesLaunchTemplate, want: "autoscaling_group_uses_launch_template"},
		{name: "RelationshipAutoScalingGroupUsesSubnet", got: RelationshipAutoScalingGroupUsesSubnet, want: "autoscaling_group_uses_subnet"},
		{name: "RelationshipAutoScalingLifecycleHookTargetsGroup", got: RelationshipAutoScalingLifecycleHookTargetsGroup, want: "autoscaling_lifecycle_hook_targets_group"},
		{name: "RelationshipAutoScalingPolicyTargetsGroup", got: RelationshipAutoScalingPolicyTargetsGroup, want: "autoscaling_policy_targets_group"},
		{name: "RelationshipAutoScalingScheduledActionTargetsGroup", got: RelationshipAutoScalingScheduledActionTargetsGroup, want: "autoscaling_scheduled_action_targets_group"},
		{name: "RelationshipBatchComputeEnvironmentUsesIAMRole", got: RelationshipBatchComputeEnvironmentUsesIAMRole, want: "batch_compute_environment_uses_iam_role"},
		{name: "RelationshipBatchComputeEnvironmentUsesLaunchTemplate", got: RelationshipBatchComputeEnvironmentUsesLaunchTemplate, want: "batch_compute_environment_uses_launch_template"},
		{name: "RelationshipBatchComputeEnvironmentUsesSecurityGroup", got: RelationshipBatchComputeEnvironmentUsesSecurityGroup, want: "batch_compute_environment_uses_security_group"},
		{name: "RelationshipBatchComputeEnvironmentUsesSubnet", got: RelationshipBatchComputeEnvironmentUsesSubnet, want: "batch_compute_environment_uses_subnet"},
		{name: "RelationshipBatchJobDefinitionReferencesSecret", got: RelationshipBatchJobDefinitionReferencesSecret, want: "batch_job_definition_references_secret"},
		{name: "RelationshipBatchJobDefinitionUsesIAMRole", got: RelationshipBatchJobDefinitionUsesIAMRole, want: "batch_job_definition_uses_iam_role"},
		{name: "RelationshipBatchJobDefinitionUsesImage", got: RelationshipBatchJobDefinitionUsesImage, want: "batch_job_definition_uses_image"},
		{name: "RelationshipBatchJobQueueUsesComputeEnvironment", got: RelationshipBatchJobQueueUsesComputeEnvironment, want: "batch_job_queue_uses_compute_environment"},
		{name: "RelationshipEC2InstanceUsesAMI", got: RelationshipEC2InstanceUsesAMI, want: "ec2_instance_uses_ami"},
		{name: "RelationshipEC2NetworkInterfaceAttachedToResource", got: RelationshipEC2NetworkInterfaceAttachedToResource, want: "ec2_network_interface_attached_to_resource"},
		{name: "RelationshipEC2NetworkInterfaceInSubnet", got: RelationshipEC2NetworkInterfaceInSubnet, want: "ec2_network_interface_in_subnet"},
		{name: "RelationshipEC2NetworkInterfaceInVPC", got: RelationshipEC2NetworkInterfaceInVPC, want: "ec2_network_interface_in_vpc"},
		{name: "RelationshipEC2NetworkInterfaceUsesSecurityGroup", got: RelationshipEC2NetworkInterfaceUsesSecurityGroup, want: "ec2_network_interface_uses_security_group"},
		{name: "RelationshipEC2SecurityGroupHasRule", got: RelationshipEC2SecurityGroupHasRule, want: "ec2_security_group_has_rule"},
		{name: "RelationshipEC2SecurityGroupInVPC", got: RelationshipEC2SecurityGroupInVPC, want: "ec2_security_group_in_vpc"},
		{name: "RelationshipEC2SubnetInVPC", got: RelationshipEC2SubnetInVPC, want: "ec2_subnet_in_vpc"},
		{name: "RelationshipEC2VolumeUsesKMSKey", got: RelationshipEC2VolumeUsesKMSKey, want: "ec2_volume_uses_kms_key"},
		{name: "RelationshipECSServiceTargetsLoadBalancer", got: RelationshipECSServiceTargetsLoadBalancer, want: "ecs_service_targets_load_balancer"},
		{name: "RelationshipECSServiceUsesTaskDefinition", got: RelationshipECSServiceUsesTaskDefinition, want: "ecs_service_uses_task_definition"},
		{name: "RelationshipECSTaskDefinitionUsesImage", got: RelationshipECSTaskDefinitionUsesImage, want: "ecs_task_definition_uses_image"},
		{name: "RelationshipECSTaskUsesNetworkInterface", got: RelationshipECSTaskUsesNetworkInterface, want: "ecs_task_uses_network_interface"},
		{name: "RelationshipEKSAddonUsesIAMRole", got: RelationshipEKSAddonUsesIAMRole, want: "eks_addon_uses_iam_role"},
		{name: "RelationshipEKSClusterHasAddon", got: RelationshipEKSClusterHasAddon, want: "eks_cluster_has_addon"},
		{name: "RelationshipEKSClusterHasNodegroup", got: RelationshipEKSClusterHasNodegroup, want: "eks_cluster_has_nodegroup"},
		{name: "RelationshipEKSClusterHasOIDCProvider", got: RelationshipEKSClusterHasOIDCProvider, want: "eks_cluster_has_oidc_provider"},
		{name: "RelationshipEKSClusterUsesIAMRole", got: RelationshipEKSClusterUsesIAMRole, want: "eks_cluster_uses_iam_role"},
		{name: "RelationshipEKSClusterUsesSecurityGroup", got: RelationshipEKSClusterUsesSecurityGroup, want: "eks_cluster_uses_security_group"},
		{name: "RelationshipEKSClusterUsesSubnet", got: RelationshipEKSClusterUsesSubnet, want: "eks_cluster_uses_subnet"},
		{name: "RelationshipEKSNodegroupUsesIAMRole", got: RelationshipEKSNodegroupUsesIAMRole, want: "eks_nodegroup_uses_iam_role"},
		{name: "RelationshipEKSNodegroupUsesSubnet", got: RelationshipEKSNodegroupUsesSubnet, want: "eks_nodegroup_uses_subnet"},
		{name: "RelationshipLambdaAliasTargetsFunction", got: RelationshipLambdaAliasTargetsFunction, want: "lambda_alias_targets_function"},
		{name: "RelationshipLambdaEventSourceMappingTargetsFunction", got: RelationshipLambdaEventSourceMappingTargetsFunction, want: "lambda_event_source_mapping_targets_function"},
		{name: "RelationshipLambdaFunctionUsesExecutionRole", got: RelationshipLambdaFunctionUsesExecutionRole, want: "lambda_function_uses_execution_role"},
		{name: "RelationshipLambdaFunctionUsesImage", got: RelationshipLambdaFunctionUsesImage, want: "lambda_function_uses_image"},
		{name: "RelationshipLambdaFunctionUsesSecurityGroup", got: RelationshipLambdaFunctionUsesSecurityGroup, want: "lambda_function_uses_security_group"},
		{name: "RelationshipLambdaFunctionUsesSubnet", got: RelationshipLambdaFunctionUsesSubnet, want: "lambda_function_uses_subnet"},
		{name: "RelationshipLightsailInstanceAttachedToDisk", got: RelationshipLightsailInstanceAttachedToDisk, want: "lightsail_instance_attached_to_disk"},
		{name: "RelationshipLightsailInstanceAttachedToStaticIP", got: RelationshipLightsailInstanceAttachedToStaticIP, want: "lightsail_instance_attached_to_static_ip"},
		{name: "RelationshipLightsailLoadBalancerTargetsInstance", got: RelationshipLightsailLoadBalancerTargetsInstance, want: "lightsail_load_balancer_targets_instance"},
		{name: "ResourceTypeAutoScalingLaunchConfiguration", got: ResourceTypeAutoScalingLaunchConfiguration, want: "aws_autoscaling_launch_configuration"},
		{name: "ResourceTypeAutoScalingLifecycleHook", got: ResourceTypeAutoScalingLifecycleHook, want: "aws_autoscaling_lifecycle_hook"},
		{name: "ResourceTypeAutoScalingPolicy", got: ResourceTypeAutoScalingPolicy, want: "aws_autoscaling_policy"},
		{name: "ResourceTypeAutoScalingScheduledAction", got: ResourceTypeAutoScalingScheduledAction, want: "aws_autoscaling_scheduled_action"},
		{name: "ResourceTypeBatchComputeEnvironment", got: ResourceTypeBatchComputeEnvironment, want: "aws_batch_compute_environment"},
		{name: "ResourceTypeBatchJob", got: ResourceTypeBatchJob, want: "aws_batch_job"},
		{name: "ResourceTypeBatchJobDefinition", got: ResourceTypeBatchJobDefinition, want: "aws_batch_job_definition"},
		{name: "ResourceTypeBatchJobQueue", got: ResourceTypeBatchJobQueue, want: "aws_batch_job_queue"},
		{name: "ResourceTypeBatchSchedulingPolicy", got: ResourceTypeBatchSchedulingPolicy, want: "aws_batch_scheduling_policy"},
		{name: "ResourceTypeEC2AMI", got: ResourceTypeEC2AMI, want: "aws_ec2_ami"},
		{name: "ResourceTypeEC2Instance", got: ResourceTypeEC2Instance, want: "aws_ec2_instance"},
		{name: "ResourceTypeEC2LaunchTemplate", got: ResourceTypeEC2LaunchTemplate, want: "aws_ec2_launch_template"},
		{name: "ResourceTypeEC2NetworkInterface", got: ResourceTypeEC2NetworkInterface, want: "aws_ec2_network_interface"},
		{name: "ResourceTypeEC2SecurityGroup", got: ResourceTypeEC2SecurityGroup, want: "aws_ec2_security_group"},
		{name: "ResourceTypeEC2SecurityGroupRule", got: ResourceTypeEC2SecurityGroupRule, want: "aws_ec2_security_group_rule"},
		{name: "ResourceTypeEC2Subnet", got: ResourceTypeEC2Subnet, want: "aws_ec2_subnet"},
		{name: "ResourceTypeEC2VPC", got: ResourceTypeEC2VPC, want: "aws_ec2_vpc"},
		{name: "ResourceTypeEC2Volume", got: ResourceTypeEC2Volume, want: "aws_ec2_volume"},
		{name: "ResourceTypeECRLifecyclePolicy", got: ResourceTypeECRLifecyclePolicy, want: "aws_ecr_lifecycle_policy"},
		{name: "ResourceTypeECRRepository", got: ResourceTypeECRRepository, want: "aws_ecr_repository"},
		{name: "ResourceTypeECSCluster", got: ResourceTypeECSCluster, want: "aws_ecs_cluster"},
		{name: "ResourceTypeECSService", got: ResourceTypeECSService, want: "aws_ecs_service"},
		{name: "ResourceTypeECSTask", got: ResourceTypeECSTask, want: "aws_ecs_task"},
		{name: "ResourceTypeECSTaskDefinition", got: ResourceTypeECSTaskDefinition, want: "aws_ecs_task_definition"},
		{name: "ResourceTypeEKSAddon", got: ResourceTypeEKSAddon, want: "aws_eks_addon"},
		{name: "ResourceTypeEKSCluster", got: ResourceTypeEKSCluster, want: "aws_eks_cluster"},
		{name: "ResourceTypeEKSNodegroup", got: ResourceTypeEKSNodegroup, want: "aws_eks_nodegroup"},
		{name: "ResourceTypeEKSOIDCProvider", got: ResourceTypeEKSOIDCProvider, want: "aws_eks_oidc_provider"},
		{name: "ResourceTypeLambdaAlias", got: ResourceTypeLambdaAlias, want: "aws_lambda_alias"},
		{name: "ResourceTypeLambdaEventSourceMapping", got: ResourceTypeLambdaEventSourceMapping, want: "aws_lambda_event_source_mapping"},
		{name: "ResourceTypeLambdaFunction", got: ResourceTypeLambdaFunction, want: "aws_lambda_function"},
		{name: "ResourceTypeLightsailDatabase", got: ResourceTypeLightsailDatabase, want: "aws_lightsail_database"},
		{name: "ResourceTypeLightsailDisk", got: ResourceTypeLightsailDisk, want: "aws_lightsail_disk"},
		{name: "ResourceTypeLightsailInstance", got: ResourceTypeLightsailInstance, want: "aws_lightsail_instance"},
		{name: "ResourceTypeLightsailLoadBalancer", got: ResourceTypeLightsailLoadBalancer, want: "aws_lightsail_load_balancer"},
		{name: "ResourceTypeLightsailStaticIP", got: ResourceTypeLightsailStaticIP, want: "aws_lightsail_static_ip"},
		{name: "ServiceAutoScaling", got: ServiceAutoScaling, want: "autoscaling"},
		{name: "ServiceBatch", got: ServiceBatch, want: "batch"},
		{name: "ServiceEC2", got: ServiceEC2, want: "ec2"},
		{name: "ServiceECR", got: ServiceECR, want: "ecr"},
		{name: "ServiceECS", got: ServiceECS, want: "ecs"},
		{name: "ServiceEKS", got: ServiceEKS, want: "eks"},
		{name: "ServiceLambda", got: ServiceLambda, want: "lambda"},
		{name: "ServiceLightsail", got: ServiceLightsail, want: "lightsail"},
	}
	for _, tt := range tests {
		tt, name := tt, tt.name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if tt.got != tt.want {
				t.Fatalf("%s = %q, want %q (byte-identical to pre-consolidation awscloud value)", tt.name, tt.got, tt.want)
			}
		})
	}
}
