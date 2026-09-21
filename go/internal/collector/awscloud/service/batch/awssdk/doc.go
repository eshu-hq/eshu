// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Batch client to the Batch scanner
// contract.
//
// The package owns Batch pagination, batched describe calls, SDK response
// mapping, AWS API telemetry, throttle detection, and pagination spans. It is
// metadata-only: the accepted apiClient surface excludes SubmitJob, CancelJob,
// TerminateJob, RegisterJobDefinition, and every Create/Update/Delete
// operation by construction, proven by a reflective guard test. Container
// command lists, job parameters, scheduling-policy fair-share state, and
// resolved secret values never cross the adapter boundary.
package awssdk
