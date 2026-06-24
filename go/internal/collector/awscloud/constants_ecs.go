// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

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
