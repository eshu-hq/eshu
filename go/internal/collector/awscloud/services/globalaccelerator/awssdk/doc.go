// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK for Go v2 Global Accelerator APIs into the
// metadata-only globalaccelerator scanner port.
//
// Client pages accelerators, their listeners, and each listener's endpoint
// groups, and reads tags for ARN-addressable accelerators, returning one nested
// topology snapshot. The adapter maps only safe control-plane fields and pins
// its client region to us-west-2 because the Global Accelerator control plane is
// reachable only there. The accepted API surface contains only List operations;
// a reflective guard test fails the build if a mutation call becomes reachable.
// Callers receive errors from AWS pagination and tag reads with the original
// cause preserved.
package awssdk
