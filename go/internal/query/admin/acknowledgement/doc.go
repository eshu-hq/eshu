// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package acknowledgement holds the operator acknowledgement of a collector
// terminal failure (issue #8830): the admin
// POST /api/v0/admin/collector-failures/acknowledge route and the admin-only
// POST /api/v0/admin/collector-failures/acknowledgements/query read, and the
// admin-only POST /api/v0/admin/collector-failures/query discovery read of one
// instance's failed scopes (no status surface carries a scope id). An
// acknowledgement says one failure on one collector instance and scope is known
// and expected. Collector status then reports the instance partial, never
// healthy, with the failure kept as an acknowledged sub-record, until the
// failure changes class or fails after a completed work item. The
// acknowledgement rows and the status read live in storage/postgres; this
// package owns the request contract, the authorization, the replay idempotency
// ledger claim, the audit events, and the outcome telemetry.
package acknowledgement
