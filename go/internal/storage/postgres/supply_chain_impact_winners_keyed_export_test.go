// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// WinnerSelectSQLForTest exposes the shipped winners select to the external
// test package, so a guard can read the payload fields it depends on.
var WinnerSelectSQLForTest = supplyChainImpactWinnerSelectSQL

// WinnersKeyedActiveFactsSQLForTest exposes the statement that builds the keyed
// active_facts temp table from the analyzed identifier temp table, so a plan
// test can assert the once-per-triple probe.
const WinnersKeyedActiveFactsSQLForTest = buildWinnersKeyedActiveFactsSQL

// WinnersKeyedActiveFactsTableForTest names the temp table that statement
// creates, so a test can drop it between two explains on one connection.
const WinnersKeyedActiveFactsTableForTest = winnersKeyedActiveFactsTable

// WinnersKeyedIdentsTableForTest names the identifier temp table that statement
// reads.
const WinnersKeyedIdentsTableForTest = winnersKeyedIdentsTable

// ActivationPointerMoveSQLForTest exposes the projector Ack statement that
// moves a scope's active pointer and appends the old and new generation to the
// winners dirty log, so a live proof runs the production capture site.
var ActivationPointerMoveSQLForTest = updateProjectorScopeGenerationQuery

// FenceNullActivePointerSQLForTest exposes the retirement fence statement that
// nulls the active pointers of retired partitions and captures their prior
// generations.
var FenceNullActivePointerSQLForTest = fenceNullActivePointerQuery

// FailProjectorWorkSQLForTest exposes the projector Fail statement that fails a
// generation and nulls the pointer it held (the reconcile path).
var FailProjectorWorkSQLForTest = failProjectorWorkQuery

// UpsertIngestionScopeSQLForTest exposes the collector commit statement whose
// conflict arm moves the active pointer when the incoming one is not NULL.
const UpsertIngestionScopeSQLForTest = upsertIngestionScopeQuery
