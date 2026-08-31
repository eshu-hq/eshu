// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package rds builds RDS posture-materialization reducer intents from one
// immutable scope generation: an rds_instance_posture presence trigger
// anchored to the earliest matching fact. Root projector assembly owns lookup
// construction and lifetime, invocation order, queue writes, retries, and
// telemetry.
package rds
