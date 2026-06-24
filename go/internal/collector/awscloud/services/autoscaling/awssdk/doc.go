// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Auto Scaling client to the Auto
// Scaling scanner contract.
//
// The package owns Auto Scaling pagination, the per-group lifecycle-hook
// describe fan-out, SDK response mapping, AWS API telemetry, throttle
// detection, and pagination spans. It is metadata-only: the accepted apiClient
// surface excludes CreateAutoScalingGroup, UpdateAutoScalingGroup,
// DeleteAutoScalingGroup, SetDesiredCapacity,
// TerminateInstanceInAutoScalingGroup, and every Create/Update/Delete/Set
// operation by construction, proven by a reflective guard test. Launch
// configuration and launch template UserData and lifecycle-hook notification
// metadata never cross the adapter boundary.
package awssdk
