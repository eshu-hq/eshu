// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package ec2 emits AWS EC2 network topology fact evidence.
//
// Alongside the raw aws_resource and aws_relationship facts for VPCs, subnets,
// security groups, security-group rules, and network interfaces, the scanner
// emits one normalized aws_security_group_rule posture fact per rule. That fact
// carries the reachability tuple (group, direction, protocol, port range,
// normalized source) plus metadata-only derived booleans (is_internet for an
// exact open CIDR, is_all_protocols, is_all_ports). It is built from the same
// rule data already fetched for the raw facts, so it adds no AWS API calls, and
// it writes no graph edges: edge projection and internet-exposure analysis are
// later reducer and query slices.
//
// The scanner also emits one metadata-only ec2_instance_posture fact per
// instance from the existing DescribeInstances pass: IMDS settings
// (IMDSv2-required, hop limit, endpoint state), user-data PRESENCE (a boolean
// only, never the content), detailed monitoring, EBS optimization, public-IP
// association, the attached instance-profile ARN, per-volume block-device
// metadata, and tenancy / Nitro-enclave state. It reads no user-data content,
// console output, or any other instance payload, adds no per-instance API
// fan-out, and emits no graph edges; reducers own the profile, KMS, and
// internet-exposure joins.
//
// The scanner also emits (#5448) one aws_resource identity fact per instance
// (resource_type=aws_ec2_instance) carrying the launch AMI id, and, when an
// AMI id is present, one aws_relationship fact recording the instance->AMI
// usage. Both are built from the same DescribeInstances entry as the posture
// fact, adding no AWS API call. The identity fact is deliberately scoped to
// identity + ami_id only: it never carries any property the posture fact and
// its CloudResource node materialization already own (see
// go/internal/reducer/ec2_instance_identity_materialization.go for the
// dual-writer safety argument). The instance->AMI relationship has no AMI
// graph node to resolve against yet, so it stays Postgres-only.
//
// The scanner also emits metadata-only aws_ec2_volume resource facts and
// reported volume-to-KMS relationship facts from one boundary-scoped
// DescribeVolumes pass. Those facts are source evidence only; reducers join
// instance block-device volume IDs to this evidence before deriving EC2
// block-device/KMS posture.
package ec2
