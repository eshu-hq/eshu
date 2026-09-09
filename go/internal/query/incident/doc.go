// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package incident serves the incident-context HTTP read (Issue #6060,
// lane B S2): the IncidentHandler behind GET
// /api/v0/incidents/{incident_id}/context, the scoped-token authorization
// boundary, and the answer-packet companion.
//
// The response types live in incident/model/, the Postgres reads in
// incident/store/, the query text in incident/sql/. This package imports
// the model leaf plus queryauth, querycontract, queryspan, and telemetry;
// it MUST NOT import the query root or incident/store/ (cycle through the
// root compatibility aliases).
package incident
