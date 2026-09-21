// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceELB identifies the regional Classic Load Balancing (ELB v1) metadata
	// scan slice. It is distinct from ServiceELBv2, which scans Application and
	// Network Load Balancers. A claim is scanned by exactly one of the two slices;
	// they never share a service_kind.
	ServiceELB = "elb"
)

const (
	// ResourceTypeELBLoadBalancer identifies a Classic (v1) Elastic Load
	// Balancer. Classic ELBs have no AWS-assigned ARN, so the scanner synthesizes
	// a partition-aware
	// arn:<partition>:elasticloadbalancing:<region>:<account>:loadbalancer/<name>
	// ARN for the resource node and for ARN-equality joins.
	ResourceTypeELBLoadBalancer = "aws_elb_load_balancer"
)

const (
	// RelationshipELBLoadBalancerRegistersInstance records a Classic ELB's
	// reported registered EC2 instance. The edge targets aws_ec2_instance by the
	// bare instance id (i-...) the EC2 instance surface publishes.
	RelationshipELBLoadBalancerRegistersInstance = "elb_load_balancer_registers_instance"
	// RelationshipELBLoadBalancerInSubnet records a Classic ELB's reported subnet
	// placement. The edge targets aws_ec2_subnet by the bare subnet id
	// (subnet-...) the EC2 scanner publishes.
	RelationshipELBLoadBalancerInSubnet = "elb_load_balancer_in_subnet"
	// RelationshipELBLoadBalancerUsesSecurityGroup records a Classic ELB's
	// reported security group attachment. The edge targets aws_ec2_security_group
	// by the bare security group id (sg-...) the EC2 scanner publishes.
	RelationshipELBLoadBalancerUsesSecurityGroup = "elb_load_balancer_uses_security_group"
	// RelationshipELBLoadBalancerInVPC records a Classic ELB's reported VPC
	// placement. The edge targets aws_ec2_vpc by the bare VPC id (vpc-...) the EC2
	// scanner publishes.
	RelationshipELBLoadBalancerInVPC = "elb_load_balancer_in_vpc"
	// RelationshipELBLoadBalancerUsesACMCertificate records an HTTPS/SSL listener's
	// reported ACM server certificate. The edge targets aws_acm_certificate by the
	// certificate ARN the ACM scanner publishes.
	RelationshipELBLoadBalancerUsesACMCertificate = "elb_load_balancer_uses_acm_certificate"
	// RelationshipELBLoadBalancerUsesIAMServerCertificate records an HTTPS/SSL
	// listener's reported IAM server certificate. The edge targets
	// aws_iam_server_certificate by the certificate ARN. IAM server certificates
	// are a forward reference (no scanner emits them yet); the value is documented
	// in relguard.KnownTargetTypeAllowlist.
	RelationshipELBLoadBalancerUsesIAMServerCertificate = "elb_load_balancer_uses_iam_server_certificate"
)
