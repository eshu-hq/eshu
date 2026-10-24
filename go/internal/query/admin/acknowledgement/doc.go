// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package acknowledgement holds the operator acknowledgement of a collector
// terminal failure (issue #8830): the admin
// POST /api/v0/admin/collector-terminal-failure-acknowledgements route. An
// acknowledgement says one failure on one collector instance and scope is
// known and expected. The collector status read then reports it as
// acknowledged instead of degrading the instance, until a failure of another
// class or a failure after a completed item appears. The acknowledgement rows
// and the status read live in storage/postgres; this package owns only the
// request contract, the authorization, the audit events, and the outcome
// telemetry.
package acknowledgement
