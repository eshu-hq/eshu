// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package membershipstore persists repository selection observations in
// repository_selection_observations (#7625) and implements membership.Store
// for the git collector's githubOrg listing evaluation.
//
// ObservationStore has four operations:
//
//   - KnownScopes reads one org's git repository scopes from
//     ingestion_scopes, matching the repo slug org case-insensitively,
//     optionally requiring the stored remote_url host, and excluding
//     repository_ref scopes. It takes no row locks.
//   - Observations reads every stored row of one selector.
//   - UpsertObservations writes one evaluation: rows are validated, sorted by
//     scope_id, and inserted in that order, so concurrent replicas of one
//     selector lock rows in the same order and cannot deadlock. A stored row
//     advances only when the batch's evaluated_at is later than its own, so a
//     replay or a lagging replica writes nothing. The counter math matches
//     membership's projection; a live test asserts the two agree.
//   - DeleteExpiredObservations (#7774) deletes rows of any selector that
//     stayed expired for the grace past their own liveness window, in bounded
//     batches of one statement each, skipping rows another transaction holds.
//     It never deletes a not_listed row, which the mass-miss guard reads.
//
// The store deletes only those long-expired observation rows, never a live
// or not_listed one, and never writes ingestion_scopes or the graph. It must
// not import the parent postgres package.
package membershipstore
