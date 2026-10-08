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
