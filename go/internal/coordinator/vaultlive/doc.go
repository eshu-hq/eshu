// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package vaultlive plans workflow rows for configured Vault metadata targets.
//
// WorkPlanner validates one enabled, claim-capable collector instance and
// returns deterministic workflow rows without resolving credentials or
// contacting Vault. The parent coordinator owns scheduling, admission,
// retries, and telemetry.
package vaultlive
