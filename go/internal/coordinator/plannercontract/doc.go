// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package plannercontract provides dependency-neutral validation shared by
// workflow scheduler planners.
//
// ValidateSafePlanKey accepts a non-blank key made from ASCII letters, digits,
// dots, underscores, and hyphens. It checks a whitespace-trimmed view but does
// not normalize the caller's value. The coordinator package retains ownership
// of scheduler order, request types, durable admission, retries, and telemetry.
package plannercontract
