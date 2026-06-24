// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package vpc emits AWS VPC network-fabric metadata fact evidence.
//
// The package owns the resources that sit alongside the EC2 instance and ENI
// surface: route tables, internet gateways, NAT gateways, network ACLs, VPC
// peering connections, VPC endpoints, Elastic IPs, DHCP option sets, customer
// gateways, virtual private gateways, and site-to-site VPN connections. VPCs,
// subnets, security groups, security group rules, and network interfaces stay
// with `services/ec2` so each AWS-reported identity has exactly one owner. VPC
// scanner relationship edges cross back to those EC2-owned resources by
// identifier when the AWS API reports both sides directly.
package vpc
