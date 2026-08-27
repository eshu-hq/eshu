// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package sbomattestation plans workflow rows for configured hosted SBOM and
// attestation targets.
//
// WorkPlanner validates one enabled, claim-capable collector instance and
// returns deterministic workflow rows without resolving credentials or reading
// artifacts. The parent coordinator owns scheduling, admission, retries, and
// telemetry.
package sbomattestation
