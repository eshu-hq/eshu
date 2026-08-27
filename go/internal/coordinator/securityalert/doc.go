// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package securityalert plans workflow rows for configured provider security
// alert targets.
//
// WorkPlanner validates one enabled, claim-capable collector instance and
// returns deterministic workflow rows without resolving credentials or calling
// a provider. The parent coordinator package owns scheduling order, durable
// admission, persistence, retries, and telemetry.
package securityalert
