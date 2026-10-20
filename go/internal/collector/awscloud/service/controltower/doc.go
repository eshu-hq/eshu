// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package controltower maps AWS Control Tower landing-zone, enabled-control, and
// enabled-baseline metadata into AWS cloud collector facts.
//
// The scanner emits reported-confidence resources for the landing zone, each
// enabled control, and each enabled baseline, plus relationships for
// control-governs-target and baseline-governs-target (keyed to the bare
// Organizations id the organizations scanner publishes) and baseline-for-
// landing-zone (internal to this scanner). The landing-zone manifest JSON body,
// control parameter values, baseline parameter values, and every enable,
// disable, reset, create, update, or delete API stay outside this package
// contract: the scanner is metadata-only.
package controltower
