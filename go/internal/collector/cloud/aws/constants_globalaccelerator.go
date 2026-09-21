// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceGlobalAccelerator identifies the AWS Global Accelerator metadata
	// scan slice. Global Accelerator is a global-endpoint service whose
	// control-plane API lives only in us-west-2, so the scanner is scoped to a
	// us-west-2 claim even though the accelerators it reports are global.
	ServiceGlobalAccelerator = "globalaccelerator"
)

const (
	// ResourceTypeGlobalAcceleratorAccelerator identifies a Global Accelerator
	// accelerator metadata resource.
	ResourceTypeGlobalAcceleratorAccelerator = "aws_globalaccelerator_accelerator"
	// ResourceTypeGlobalAcceleratorListener identifies a Global Accelerator
	// listener metadata resource.
	ResourceTypeGlobalAcceleratorListener = "aws_globalaccelerator_listener"
	// ResourceTypeGlobalAcceleratorEndpointGroup identifies a Global
	// Accelerator endpoint group metadata resource.
	ResourceTypeGlobalAcceleratorEndpointGroup = "aws_globalaccelerator_endpoint_group"
	// ResourceTypeGlobalAcceleratorEndpoint identifies a Global Accelerator
	// endpoint metadata resource. Endpoints reference an ALB/NLB, an Elastic IP
	// allocation, or an EC2 instance by id; the endpoint resource records that
	// reference without claiming ownership of the referenced resource.
	ResourceTypeGlobalAcceleratorEndpoint = "aws_globalaccelerator_endpoint"
)

const (
	// RelationshipGlobalAcceleratorAcceleratorHasListener records listener
	// membership on an accelerator.
	RelationshipGlobalAcceleratorAcceleratorHasListener = "globalaccelerator_accelerator_has_listener"
	// RelationshipGlobalAcceleratorListenerHasEndpointGroup records endpoint
	// group membership on a listener.
	RelationshipGlobalAcceleratorListenerHasEndpointGroup = "globalaccelerator_listener_has_endpoint_group"
	// RelationshipGlobalAcceleratorEndpointGroupHasEndpoint records endpoint
	// membership in an endpoint group.
	RelationshipGlobalAcceleratorEndpointGroupHasEndpoint = "globalaccelerator_endpoint_group_has_endpoint"
	// RelationshipGlobalAcceleratorEndpointTargetsResource records the
	// reported endpoint target (ALB/NLB load balancer, Elastic IP allocation,
	// or EC2 instance) an endpoint routes traffic to. The target_type names the
	// referenced resource family so downstream correlation can join the edge.
	RelationshipGlobalAcceleratorEndpointTargetsResource = "globalaccelerator_endpoint_targets_resource"
)
