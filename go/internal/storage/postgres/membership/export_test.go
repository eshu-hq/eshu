// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membershipstore

// SchemaSQL exposes schemaSQL so external tests can check it against the
// embedded migration.
const SchemaSQL = schemaSQL

// KnownScopesQuery exposes knownScopesQuery so external tests can assert on
// its partition predicates.
const KnownScopesQuery = knownScopesQuery

// UpsertObservationsQuery exposes upsertObservationsQuery so external tests
// can assert on its shape.
const UpsertObservationsQuery = upsertObservationsQuery

// DeleteExpiredObservationsQuery exposes deleteExpiredObservationsQuery so
// external tests can assert on its expiry predicate and locking.
const DeleteExpiredObservationsQuery = deleteExpiredObservationsQuery

// ExpiredSweepBatchSize and ExpiredSweepMaxBatches expose the sweep bounds.
const (
	ExpiredSweepBatchSize  = expiredSweepBatchSize
	ExpiredSweepMaxBatches = expiredSweepMaxBatches
)

// LiveScopeObservationsQuery exposes liveScopeObservationsQuery so external
// tests can assert on its live filter.
const LiveScopeObservationsQuery = liveScopeObservationsQuery
