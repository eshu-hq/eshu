// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK for Go v2 Shield Advanced APIs into the
// metadata-only shield scanner port.
//
// Client pages protections through ListProtections and reads the per-account
// subscription summary through DescribeSubscription plus GetSubscriptionState
// for the canonical state. It maps only safe identity and state fields:
// subscription limits, time commitment, start/end times, and proactive
// engagement status are dropped. The adapter pins its client region to
// us-east-1 because the Shield control plane is reachable only there, and
// returns a nil subscription when the account has none (ResourceNotFound). The
// accepted API surface contains only List/Describe/Get reads; a reflective
// guard test fails the build if a mutation call becomes reachable. Callers
// receive errors from AWS pagination and subscription reads with the original
// cause preserved.
package awssdk
