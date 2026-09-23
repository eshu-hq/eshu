// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package decisionsstore persists reducer-owned projection decisions and
// their evidence in PostgreSQL.
//
// DecisionStore owns two tables: projection_decisions (one row per
// materialization decision, keyed by decision_id) and
// projection_decision_evidence (evidence rows referencing a decision_id, with
// an optional fact_id). UpsertDecision and InsertEvidence write with
// ON CONFLICT DO UPDATE so repeated delivery of the same decision or evidence
// row is idempotent; ListDecisions and ListEvidence read back in
// (created_at, id) order. ListDecisions clamps a non-positive Limit to 1. The
// DDL and statement text live beside the store and moved byte-identically
// with it. This package must not import the parent postgres package.
package decisionsstore
