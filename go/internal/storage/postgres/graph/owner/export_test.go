// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ownerstore

// CloudResourceOwnerBackfillKey exposes the backfill completion marker's
// private stable key to backfill_test.go, which lives in package
// ownerstore_test (it also needs postgres.MigrationSQL, so it must be an
// external test package). Production code never needs this key outside the
// backfill store itself.
const CloudResourceOwnerBackfillKey = cloudResourceOwnerBackfillKey
