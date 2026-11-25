// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// WinnerSelectSQLForTest exposes the shipped winners select to the external
// test package, so a guard can read the payload fields it depends on.
var WinnerSelectSQLForTest = supplyChainImpactWinnerSelectSQL

// WinnerBodySQLForTest exposes the shipped winner select body to the
// external test package, so the M6 narrow-store proof can compose it over a
// shape-built active-facts table. The body is byte-identical to the shipped
// select's tail: a digest match proves the shape preserves winner truth.
var WinnerBodySQLForTest = supplyChainImpactWinnerBodySQL

// WinnersKeyedActiveFactsSQLForTest exposes the statement that builds the keyed
// active_facts temp table from the analyzed identifier temp table, so a plan
// test can assert the once-per-identifier probe on the keyed winners index.
const WinnersKeyedActiveFactsSQLForTest = buildWinnersKeyedActiveFactsSQL

// WinnersKeyedScannedCopiesSQLForTest exposes the pre-filter scanned-copies
// COUNT(*) over the same identifier temp table, so a plan test can assert it
// takes the same keyed-index path instead of scanning fact_records (#11343).
// The export derives from the shipped constant, so the explained statement is
// the statement the tick runs.
const WinnersKeyedScannedCopiesSQLForTest = countWinnersKeyedScannedCopiesSQL

// WinnersKeyedActiveFactsTableForTest names the temp table that statement
// creates, so a test can drop it between two explains on one connection.
const WinnersKeyedActiveFactsTableForTest = winnersKeyedActiveFactsTable

// WinnersKeyedIdentsTableForTest names the identifier temp table that statement
// reads.
const WinnersKeyedIdentsTableForTest = winnersKeyedIdentsTable

// WinnersKeyedNarrowBuildSQLForTest exposes the statement that persists the
// identifier probe to the narrow temp table, which the active-facts build
// reads; a proof must run it first, mirroring runKeyedOrFull.
const WinnersKeyedNarrowBuildSQLForTest = buildWinnersKeyedNarrowSQL

// WinnersKeyedNarrowAnalyzeSQLForTest exposes the ANALYZE of the narrow
// temp table, so the active-facts build plans on real statistics.
const WinnersKeyedNarrowAnalyzeSQLForTest = analyzeWinnersKeyedNarrowSQL

// ActivationPointerMoveSQLForTest exposes the projector Ack statement that
// moves a scope's active pointer and appends the old and new generation to the
// winners dirty log, so a live proof runs the production capture site.
var ActivationPointerMoveSQLForTest = updateProjectorScopeGenerationQuery

// WinnersAuditCandidateSelectSQLForTest exposes the split-scan candidate SELECT
// (#10416 slice 1), so a plan test can prove the read shape against the stored
// winners without running the audit.
var WinnersAuditCandidateSelectSQLForTest = selectWinnersAuditCandidatesSQL

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
