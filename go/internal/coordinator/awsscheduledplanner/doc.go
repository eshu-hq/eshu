// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awsscheduledplanner plans scheduled AWS collector work from a
// collector instance's configuration, independently of freshness triggers.
//
// It is the scheduled sibling of awsfreshnessplanner: that package plans from
// claimed freshness triggers, this one plans from the instance's
// scheduled_scan_enabled flag and target_scopes. The two deliberately share one
// definition of target-scope parsing — this package calls
// awsfreshnessplanner.ParseTargetScopes and TargetAuthorized rather than
// keeping a copy, so the authorization decision cannot drift between them.
//
// ScanEnabled is exported because the root coordinator service decodes the
// sibling scheduled_scan_enabled flag before deciding whether to plan at all;
// keeping a second copy at root is exactly the drift this package avoids.
//
// The package consumes internal/workflow and internal/scope and must not import
// the root coordinator package; root imports it to wire the planner port.
package awsscheduledplanner
