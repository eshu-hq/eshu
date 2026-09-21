// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package route53recoverycontrolconfig maps Amazon Route 53 Application Recovery
// Controller recovery-control configuration metadata into AWS cloud collector
// facts.
//
// The scanner emits reported-confidence resources for recovery-control clusters,
// control panels, routing controls, and safety rules, plus relationships for
// control-panel-in-cluster, routing-control-in-control-panel, and
// safety-rule-in-control-panel membership. Every relationship targets a resource
// this same scanner publishes and is keyed by the ARN that target node publishes
// as its resource_id, so no edge dangles. Routing control state (the live On/Off
// value, which lives behind the separate route53recoverycluster data plane) and
// any mutation API stay outside this package contract: the scanner is
// metadata-only.
package route53recoverycontrolconfig
