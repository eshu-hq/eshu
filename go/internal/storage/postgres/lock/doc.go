// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package lockstore serializes concurrent Postgres writers with
// transaction-level advisory locks so two ingesters, reducers, or
// projectors never interleave a commit on the same key.
//
// PackageRegistryIdentityLocker and PlatformGraphLocker wrap a unit of
// work in deterministic advisory locks over sorted package or platform ID
// sets (WithPackageRegistryIdentityLocks, WithPlatformLocks):
// lock ordering is always ascending by ID, so two transactions locking
// overlapping sets can never deadlock. The deferred-maintenance helpers
// (AcquireDeferredMaintenanceRepoSharedLock,
// AcquireDeferredMaintenanceRepoExclusiveLocks, SortedUniqueRepoKeys and
// the DeferredMaintenance* key/SQL constants) fence whole-corpus
// maintenance behind shared or exclusive per-repository partition locks.
//
// Callers construct the lockers as struct literals with a DB handle; the
// helpers take a db handle per call. This package must not import the
// parent postgres package.
package lockstore
