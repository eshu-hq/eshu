// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceELBv2 identifies the regional Elastic Load Balancing v2 service
	// scan slice.
	ServiceELBv2 = "elbv2"
)

const (
	// ResourceTypeELBv2LoadBalancer identifies an ELBv2 load balancer.
	ResourceTypeELBv2LoadBalancer = "aws_elbv2_load_balancer"
	// ResourceTypeELBv2Listener identifies an ELBv2 listener.
	ResourceTypeELBv2Listener = "aws_elbv2_listener"
	// ResourceTypeELBv2TargetGroup identifies an ELBv2 target group.
	ResourceTypeELBv2TargetGroup = "aws_elbv2_target_group"
	// ResourceTypeELBv2Rule identifies an ELBv2 listener rule.
	ResourceTypeELBv2Rule = "aws_elbv2_rule"
)

const (
	// RelationshipELBv2LoadBalancerHasListener records listener membership on a
	// load balancer.
	RelationshipELBv2LoadBalancerHasListener = "elbv2_load_balancer_has_listener"
	// RelationshipELBv2ListenerHasRule records rule membership on a listener.
	RelationshipELBv2ListenerHasRule = "elbv2_listener_has_rule"
	// RelationshipELBv2ListenerRoutesToTargetGroup records listener or rule
	// routing to a target group.
	RelationshipELBv2ListenerRoutesToTargetGroup = "elbv2_listener_routes_to_target_group"
	// RelationshipELBv2TargetGroupAttachedToLoadBalancer records target group
	// attachment to a load balancer.
	RelationshipELBv2TargetGroupAttachedToLoadBalancer = "elbv2_target_group_attached_to_load_balancer"
)
