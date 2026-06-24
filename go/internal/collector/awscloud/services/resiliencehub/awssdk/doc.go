// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK Resilience Hub control-plane API into the
// resiliencehub scanner's metadata-only Client port.
//
// The adapter reads only application, resiliency policy, application component,
// input source, and assessment list APIs, the per-application describe read, the
// published-version physical-resource list, and resource-tag reads. It never
// reads assessment result bodies, drift detail, or recommendation contents, and
// never calls a mutation, resource-import, or assessment-start API. The accepted
// SDK surface is enforced at build time by a reflection guard test over the
// apiClient interface.
package awssdk
