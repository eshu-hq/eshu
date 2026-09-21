// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK Route 53 Application Recovery Controller
// recovery-control configuration control-plane calls into the scanner-owned
// metadata model.
//
// The adapter's apiClient interface lists only the cluster, control-panel,
// routing-control, and safety-rule List reads plus ListTagsForResource. It never
// calls UpdateRoutingControlState (which lives in the separate
// route53recoverycluster data-plane module this package never imports) and never
// calls any Create/Update/Delete mutation, so the adapter cannot change routing
// control state or mutate configuration. A reflective exclusion test enforces
// that contract at build time. Each call is wrapped in the shared AWS pagination
// span and API-call/throttle counters.
package awssdk
