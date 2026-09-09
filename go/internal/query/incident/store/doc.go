// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package store holds the incident-context Postgres reads (Issue #6060,
// lane B S2): the PostgresIncidentContextStore behind the incident response
// (anchor selection, timeline and change candidates, routing, runtime, and
// review evidence), the PostgresIncidentRepositoryAuthorizer behind the
// durable owning-repository edge, the incident row decoders, and the
// incident-specific factschema decode wrappers.
//
// The response types live in incident/model/, the query text in
// incident/sql/, the HTTP surface in incident/. The store imports the model
// and sql leaves plus querycontract, the service-catalog-adjacent supply
// chain port, and the factschema SDKs; it MUST NOT import the query root or
// incident/ (cycle through the root compatibility aliases).
package store
