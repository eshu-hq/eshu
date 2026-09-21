// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

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
