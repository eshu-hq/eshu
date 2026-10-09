// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package incident serves the incident-context HTTP read (Issue #6060,
// lane B S2): the IncidentHandler behind GET
// /api/v0/incidents/{incident_id}/context, the scoped-token authorization
// boundary, and the answer-packet companion.
//
// The response types live in incident/model/, the Postgres reads in
// incident/store/, the query text in incident/sql/. This package imports
// the model leaf plus auth, querycontract, tracing, and telemetry;
// it MUST NOT import the query root or incident/store/ (cycle through the
// root compatibility aliases).
//
// A failed context read or authorization check never echoes the backend
// error: a stale or timed-out reader answers the retryable 503, a client
// cancel 499, and anything else a fixed 500 with the error recorded on the
// handler span. An authorization failure still fails closed (#7674).
package incident
