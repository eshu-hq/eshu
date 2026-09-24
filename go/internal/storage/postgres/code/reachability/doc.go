// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package reachabilitystore persists the reducer-materialized code
// reachable-set rows and root-kind verdicts the #5376/#5494 dead-code
// projection domain reads and writes.
//
// CodeReachabilityStore keys reachable-set rows and code_root_verdicts by
// active generation plus a per-repository completion watermark, so a
// standing lookup (ListLatestByEntities) serves dead-code reads without
// falling back to a compatibility scan over completed shared projection
// intents, and an empty reachable-set snapshot for a no-Ruby or
// zero-verdict repo still makes durable progress: ReplaceRepositoryRows
// always stamps the watermark, even when it writes zero rows. The watermark
// also carries CodeReachabilityVerdictSchemaEpoch so an upgraded deployment
// re-projects every already-indexed repo whose watermark predates the
// current verdict-schema epoch exactly once (LoadPendingCodeReachabilityInputs's
// upgrade-backfill predicate), without a watermark reset.
//
// The loader (LoadPendingCodeReachabilityInputs and its private per-repo
// readers) feeds codeintel.BuildCodeRootVerdicts the roots, Ruby class
// ancestry, and Rails route-fact snapshot it needs to confirm or downgrade a
// dead-code root, including the #5494 route-liveness extension that keeps a
// Rails controller action confirmed when the repo's own route surface
// cannot rule it reachable.
//
// This package must not import the parent internal/storage/postgres
// package from non-test code: leaves under storage/postgres must not depend
// on root (#6693). cmd/reducer constructs CodeReachabilityStore, and the
// applied DDL lives in the storage/postgres/migrations package. One live
// test (code_reachability_upgrade_backfill_live_test.go) stays in the
// parent postgres package because it defines per-test-suffix and live-DB
// helpers shared by unrelated root live tests; this package's own
// external route-liveness live test keeps its own copies of the same
// helpers rather than depend on that root file's test-only symbols, which
// Go does not expose across package boundaries.
package reachabilitystore
