// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceVPCLattice identifies the regional Amazon VPC Lattice metadata-only
	// scan slice. The scanner reads service network, service, target group, and
	// listener control-plane metadata through the VPC Lattice list/get management
	// APIs and never reads or persists auth-policy bodies, resource-policy bodies,
	// or any data-plane payload, and never mutates VPC Lattice state.
	ServiceVPCLattice = "vpclattice"
)

const (
	// ResourceTypeVPCLatticeServiceNetwork identifies an Amazon VPC Lattice
	// service network metadata resource. The scanner emits identity, associated
	// service/VPC/resource-configuration counts, and lifecycle timestamps only.
	ResourceTypeVPCLatticeServiceNetwork = "aws_vpclattice_service_network"
	// ResourceTypeVPCLatticeService identifies an Amazon VPC Lattice service
	// metadata resource. The scanner emits identity, status, custom domain name,
	// DNS entry domain name, auth type, and the ACM certificate ARN reference
	// only; it never reads the auth-policy body.
	ResourceTypeVPCLatticeService = "aws_vpclattice_service"
	// ResourceTypeVPCLatticeTargetGroup identifies an Amazon VPC Lattice target
	// group metadata resource. The scanner emits identity, type (IP, LAMBDA,
	// INSTANCE, ALB), protocol, port, IP address type, status, and the backing
	// VPC identifier only.
	ResourceTypeVPCLatticeTargetGroup = "aws_vpclattice_target_group"
	// ResourceTypeVPCLatticeListener identifies an Amazon VPC Lattice listener
	// metadata resource. The scanner emits identity, protocol, port, and the
	// parent service identity only; it never expands listener rule action bodies.
	ResourceTypeVPCLatticeListener = "aws_vpclattice_listener"
)

const (
	// RelationshipVPCLatticeServiceNetworkAssociatesVPC records a VPC Lattice
	// service network's association with a VPC. The target is the EC2-owned
	// aws_ec2_vpc identity keyed by the bare vpc-id the EC2 scanner publishes.
	RelationshipVPCLatticeServiceNetworkAssociatesVPC = "vpclattice_service_network_associates_vpc"
	// RelationshipVPCLatticeServiceNetworkAssociatesService records a VPC Lattice
	// service network's association with a service. The target is the VPC Lattice
	// service identity keyed by the service ARN this scanner publishes.
	RelationshipVPCLatticeServiceNetworkAssociatesService = "vpclattice_service_network_associates_service"
	// RelationshipVPCLatticeListenerInService records a VPC Lattice listener's
	// membership in its parent service. The target is the VPC Lattice service
	// identity keyed by the service ARN this scanner publishes.
	RelationshipVPCLatticeListenerInService = "vpclattice_listener_in_service"
	// RelationshipVPCLatticeTargetGroupInVPC records a VPC Lattice target group's
	// backing VPC. The target is the EC2-owned aws_ec2_vpc identity keyed by the
	// bare vpc-id the EC2 scanner publishes.
	RelationshipVPCLatticeTargetGroupInVPC = "vpclattice_target_group_in_vpc"
	// RelationshipVPCLatticeTargetGroupServesService records a VPC Lattice target
	// group's use by a service. The target is the VPC Lattice service identity
	// keyed by the service ARN this scanner publishes.
	RelationshipVPCLatticeTargetGroupServesService = "vpclattice_target_group_serves_service"
	// RelationshipVPCLatticeTargetGroupTargetsLambda records a VPC Lattice target
	// group's registered Lambda function target. The target is the
	// aws_lambda_function identity keyed by the function ARN the Lambda scanner
	// publishes.
	RelationshipVPCLatticeTargetGroupTargetsLambda = "vpclattice_target_group_targets_lambda"
	// RelationshipVPCLatticeTargetGroupTargetsInstance records a VPC Lattice
	// target group's registered EC2 instance target. The target is the
	// aws_ec2_instance identity keyed by the bare instance id (i-...).
	RelationshipVPCLatticeTargetGroupTargetsInstance = "vpclattice_target_group_targets_instance"
	// RelationshipVPCLatticeTargetGroupTargetsLoadBalancer records a VPC Lattice
	// target group's registered Application Load Balancer target. The target is
	// the aws_elbv2_load_balancer identity keyed by the load balancer ARN the
	// ELBv2 scanner publishes.
	RelationshipVPCLatticeTargetGroupTargetsLoadBalancer = "vpclattice_target_group_targets_load_balancer"
	// RelationshipVPCLatticeServiceUsesCertificate records a VPC Lattice service's
	// custom-domain ACM certificate. The target is the aws_acm_certificate
	// identity keyed by the certificate ARN the ACM scanner publishes.
	RelationshipVPCLatticeServiceUsesCertificate = "vpclattice_service_uses_certificate"
)
