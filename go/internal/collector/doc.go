// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package collector owns the seam every Eshu collector kind plugs into.
//
// It defines Source, Committer, Service, and CollectedGeneration — the contract
// a collector implements to hand facts to durable commit — plus the claimed-work
// machinery (ClaimedService, ClaimedCommitter, ClaimedSource) that roughly
// fifteen collector kinds share, the fair claim dispatcher, generation
// dead-letter records and replay completion, and the retryable-failure
// classification collectors report through.
//
// The package deliberately does NOT implement any one collector. The git
// repository collector lives in the gitrepo subpackage; cloud, registry, and
// runtime collectors live in their own subpackages beside it. Graph projection
// and query-time truth belong to the downstream projector, reducer, storage,
// and query packages.
//
// Collection is best-effort over remote and local filesystems. Callers must
// handle partial snapshots, discovery skips, webhook-triggered refreshes, claim
// fencing, collector generation dead-letter records/replay completion, and
// batch-drain hooks explicitly. Empty-batch drain hooks are opt-in for callers
// that need empty configured shards to participate in a cross-process barrier.
// Claim-aware collection copies hosted tenant boundaries from workflow work
// items into commit mutations so storage can fence fact persistence.
//
// The scannerworker subpackage owns the hosted boundary for isolated security
// analyzers. It defines claim input, target scope, resource limits,
// source-fact output validation, retry/dead-letter payloads, and the claim loop
// used by scanner-worker runtimes while reducers keep finding truth ownership.
package collector
