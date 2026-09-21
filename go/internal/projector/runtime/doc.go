// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package runtime drives one projection of one scope generation end to end:
// it admits the generation's facts, builds the canonical materialization and
// the content records, writes them through the injected writers, publishes the
// graph-projection phase checkpoints, and collects the reducer intents the
// downstream queue consumes.
//
// [Runtime] is the unit of work and [Result] is what one projection reports.
// Admission runs before any writer is touched: a fact whose schema version the
// projector does not support, or whose generation does not match the scope
// generation under projection, fails the whole projection rather than being
// written partially. Package-registry identity writes are bracketed by the
// [PackageRegistryIdentityLocker] so concurrent projections of different
// scopes cannot interleave on a shared package identity.
//
// The package borrows fact payloads rather than cloning them and must not
// mutate them: a mutation here would corrupt the caller's facts and the
// replay comparison that reads them afterwards.
package runtime
