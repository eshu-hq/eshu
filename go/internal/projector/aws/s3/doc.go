// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package s3 builds S3 bucket-posture reducer intents from one immutable scope
// generation: LOGS_TO edge readiness from access-logging targets,
// external-principal GRANTS_ACCESS_TO edges, and internet-exposure
// derivation. Root projector assembly owns lookup construction and lifetime,
// invocation order, queue writes, retries, and telemetry.
package s3
