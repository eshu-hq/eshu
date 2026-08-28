// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package scannerworker plans workflow rows for configured scanner-worker
// source and image targets.
//
// WorkPlanner validates one enabled, claim-capable collector instance and
// returns deterministic workflow rows without reading source paths or image
// artifacts. The parent coordinator owns scheduling, admission, retries, queue
// and lease behavior, and telemetry.
package scannerworker
