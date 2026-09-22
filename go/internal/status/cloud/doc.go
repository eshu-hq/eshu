// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cloud holds the AWS cloud-collector family of the status report:
// per-tuple scan status and the aggregate EventBridge/AWS Config freshness
// trigger backlog. The root internal/status package aggregates this package
// into RawSnapshot and Report, so the root imports cloud; this package must
// not import the root or any sibling leaf. The collector leaf is the one
// declared exception to that rule elsewhere: it imports cloud to fold
// AWSScanStatus rows into its unified collector runtime view.
//
// AWSScanStatus captures one AWS collector tuple status row for
// (collector_instance_id, account_id, region, service_kind): API call,
// throttle, warning, resource, relationship, and tag-observation counts,
// commit and failure state, and the lifecycle timestamps
// (LastStartedAt/LastObservedAt/LastCompletedAt/LastSuccessfulAt/UpdatedAt).
// CloneAWSScanStatuses returns a defensive copy; RenderAWSScanLines renders
// one compact operator line per row for the plain-text status surface.
//
// AWSFreshnessSnapshot captures aggregate EventBridge/AWS Config freshness
// trigger backlog state (a shared.NamedCount per lifecycle status, plus the
// oldest queued age) for the admin status surface. CloneAWSFreshnessSnapshot
// clamps the age through shared.NonNegativeDuration; RenderAWSFreshnessLines
// renders the queued/claimed/handed_off/failed counts and oldest age as one
// operator line, or nil when the snapshot carries no evidence.
//
// AWSScanJSON, AWSScansJSON, AWSFreshnessJSON, and AWSFreshnessJSONFrom carry
// the operator-facing wire contract. Their struct tags and formatting are
// part of the published status JSON consumed by the status HTTP surfaces and
// MCP status tools, and are locked by byte-for-byte goldens at
// internal/status/testdata/render_json_golden.json, render_text_golden.txt,
// and render_json_key_paths_golden.txt. A golden failure is an API break to
// justify, not a golden to regenerate reflexively.
package cloud
