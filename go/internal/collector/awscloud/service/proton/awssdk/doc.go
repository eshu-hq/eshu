// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK Proton control-plane API into the
// scanner-owned metadata the proton package consumes.
//
// The adapter pages the environment, service, and template list reads, the
// per-service GetService detail read (mapped to source-repository linkage by
// reference only), the service-instance list read (for service-in-environment
// placement), and resource-tag reads, mapping each SDK response into
// scanner-owned types. It never reads or persists spec manifest bodies, pipeline
// spec bodies, template version schema bodies, or deployment input parameter
// values, and never calls a mutation API. The accepted read surface is enforced
// at build time by exclusion_test.go. Each call is wrapped in the shared AWS
// pagination span and API-call/throttle counters.
package awssdk
