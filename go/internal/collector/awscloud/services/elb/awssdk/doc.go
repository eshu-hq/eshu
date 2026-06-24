// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK for Go v2 Classic ELB (v1) responses into
// scanner-owned ELB records. It owns SDK pagination, batched tag reads, response
// mapping, throttle classification, and per-call telemetry.
//
// The adapter is metadata-only by construction: its accepted SDK surface is
// DescribeLoadBalancers (paginated) and DescribeTags. It never calls
// DescribeInstanceHealth and exposes no mutation operation. A reflective guard
// test fails the build if any create, delete, register, deregister, attach,
// detach, or modify method becomes reachable.
package awssdk
