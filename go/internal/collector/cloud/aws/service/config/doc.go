// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package config maps AWS Config metadata into AWS cloud collector facts.
//
// The package owns scanner-level fact selection for configuration recorders
// (recorded resource-type scope), delivery channels, config rules (owner,
// managed-rule source identifier or custom-Lambda function ARN, and resource
// scope), conformance packs (deployment status and member-rule count),
// configuration aggregators (source accounts and regions), and retention
// configurations. It emits reported evidence only. Recorded configuration item
// bodies (full resource snapshots), per-resource compliance evaluation result
// bodies, and custom-rule Lambda code stay outside the scanner contract because
// they are full resource state, not control-plane metadata. Aggregate
// compliance counts are acceptable; per-resource compliance detail is not.
package config
