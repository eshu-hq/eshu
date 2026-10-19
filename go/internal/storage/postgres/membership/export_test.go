// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membershipstore

// SchemaSQL exposes schemaSQL so external tests can check it against the
// embedded migration.
const SchemaSQL = schemaSQL

// KnownScopesQuery exposes knownScopesQuery so external tests can assert on
// its partition predicates.
const KnownScopesQuery = knownScopesQuery

// KnownScopesPrefixQuery exposes knownScopesPrefixQuery so external tests
// can assert on its prefix partition predicates.
const KnownScopesPrefixQuery = knownScopesPrefixQuery

// UpsertObservationsQuery exposes upsertObservationsQuery so external tests
// can assert on its shape.
const UpsertObservationsQuery = upsertObservationsQuery

// DeleteExpiredObservationsQuery exposes deleteExpiredObservationsQuery so
// external tests can assert on its expiry predicate and locking.
const DeleteExpiredObservationsQuery = deleteExpiredObservationsQuery

// UpsertSelectorCycleQuery exposes upsertSelectorCycleQuery so external
// tests can assert on its advance-only predicate.
const UpsertSelectorCycleQuery = upsertSelectorCycleQuery

// ExpiredSweepBatchSize and ExpiredSweepMaxBatches expose the sweep bounds.
const (
	ExpiredSweepBatchSize  = expiredSweepBatchSize
	ExpiredSweepMaxBatches = expiredSweepMaxBatches
)

// LiveScopeObservationsQuery exposes liveScopeObservationsQuery so external
// tests can assert on its live filter.
const LiveScopeObservationsQuery = liveScopeObservationsQuery

// UpsertClassificationsQuery exposes upsertClassificationsQuery so external
// tests can assert on its not_listed guard and advance-only predicate.
const UpsertClassificationsQuery = upsertClassificationsQuery

// ObservationsQuery exposes observationsQuery so external tests can assert
// it selects the probe columns.
const ObservationsQuery = observationsQuery
