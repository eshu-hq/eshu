// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package globalaccelerator converts AWS Global Accelerator topology metadata
// into AWS resource and relationship facts.
//
// Scanner accepts only the globalaccelerator service boundary and emits
// metadata-only facts for accelerators, listeners, endpoint groups, and
// endpoints. Each endpoint records the resource it routes to (an ALB/NLB load
// balancer, an Elastic IP allocation, or an EC2 instance) as a typed
// relationship without claiming ownership of the referenced resource. The
// package never mutates a Global Accelerator resource and never reads beyond
// control-plane metadata, consistent with the AWS cloud scanner ADR.
package globalaccelerator
