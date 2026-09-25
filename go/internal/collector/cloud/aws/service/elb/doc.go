// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package elb emits Classic (v1) Elastic Load Balancer resource and
// relationship facts for the AWS cloud collector. Classic load balancers carry
// no AWS-assigned ARN, so the scanner synthesizes a partition-aware ARN from the
// scan boundary and the load balancer name for the resource node and for
// ARN-equality joins.
//
// The scanner emits one aws_resource per load balancer and one aws_relationship
// per reported registered EC2 instance, subnet, security group, VPC, and
// HTTPS/SSL listener certificate. It is metadata-only: it never reads or
// persists certificate bodies, private keys, or live instance health. AWS SDK
// pagination, credentials, throttling, and telemetry are owned by the awssdk
// adapter subpackage.
package elb
