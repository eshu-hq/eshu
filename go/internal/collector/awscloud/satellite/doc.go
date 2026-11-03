// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package satellite is the typed component port for the extracted AWS cloud
// satellite (eshu-hq/eshu-collector-aws-cloud, cutover from eshu-hq/eshu
// issue #8852).
//
// It owns the satellite's component identity, emitted-kind inventory,
// reducer-phase contract, and required producer-grant set, so host-side
// admission (install, verify, conform) and activation planning judge the
// same manifest the satellite publishes. It does not scan AWS APIs, emit
// facts, claim work, or write graph truth.
package satellite
