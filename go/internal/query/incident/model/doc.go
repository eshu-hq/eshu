// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package model holds the incident-context read model (Issue #6060, lane B
// S2): the IncidentContext* row, filter, snapshot, and response types, the
// capability and limit constants, and the BuildIncidentContextResponse
// assembly that applies the public contract (every expected path slot
// present, missing and ambiguous evidence derived) to a store snapshot.
//
// The Postgres reads behind the snapshots live in incident/store/, the query
// text in incident/sql/, and the HTTP surface in incident/. This package
// imports only querycontract, so every leaf can share its types without a
// cycle back through the query root.
package model
