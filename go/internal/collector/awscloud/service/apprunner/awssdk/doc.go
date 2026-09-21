// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 App Runner client to the App
// Runner scanner contract.
//
// The package owns App Runner NextToken pagination, per-resource describe
// enrichment, SDK response mapping, AWS API telemetry, throttle detection, and
// pagination spans. It is metadata-only: the accepted apiClient surface
// excludes CreateService, DeleteService, UpdateService, PauseService,
// ResumeService, StartDeployment, DeleteConnection, AssociateCustomDomain, and
// every Create/Update/Delete operation by construction, proven by a reflective
// guard test. Runtime environment-variable values and source repository
// credentials never cross the adapter boundary: only environment-variable names
// and secret-reference ARNs are mapped.
package awssdk
