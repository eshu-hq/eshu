// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package ssm builds SSM host inventory reducer intents from one immutable
// scope generation: observation-node and platform-pairing-edge
// materialization (eshu-hq/eshu#9558). Root projector assembly owns lookup
// construction and lifetime, invocation order, queue writes, retries, and
// telemetry.
package ssm
