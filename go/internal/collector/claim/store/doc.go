// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package store selects the workflow claim store a claim-aware collector
// runs against.
//
// New reads ESHU_WORKFLOW_CLAIM_PLANE_URL once at startup. Unset or blank
// returns the caller's Postgres store unchanged, so the claim path is
// unchanged; selection adds one startup log line and one counter tick. A valid http or https URL
// returns a workflow.ClaimPlaneClient with a bounded HTTP timeout, bound to
// the claim-plane request metrics. An invalid URL is a startup error naming
// the variable. There is no fallback from the claim plane to Postgres: an
// unreachable claim plane fails the collector's claim calls, which is the
// same fail-closed path a core Postgres outage takes.
//
// The package owns selection only. The ingestion commit fence (#8875 P2) and
// the claim protocol itself live in storage/postgres and workflow.
package store
