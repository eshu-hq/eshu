// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package ec2 builds EC2 instance-posture reducer intents from one immutable
// scope generation: instance-node materialization, ami_id identity
// projection, block-device KMS posture, internet exposure, and USES_PROFILE
// edge readiness. Root projector assembly owns lookup construction and
// lifetime, invocation order, queue writes, retries, and telemetry.
package ec2
