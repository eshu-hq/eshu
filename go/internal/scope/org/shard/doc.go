// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package shard holds the one definition of how a security-alert
// organization target is split into bounded shard scopes (#9019).
//
// One org claim reads at most 100 allowlisted repositories one by one after a
// truncated org read. A target with more same-org repositories is planned as
// several shard scopes of at most MaxRepositories each, so every
// repository is read within one scan interval. Partition computes the shard
// set from the target configuration alone: it is a pure function of the base
// scope_id, the organization, and the allowlist, never of allowlist order or
// stored state. The coordinator's planner, the collector, and the query layer
// each call it, so all three agree on which scope holds which repository.
//
// The partition is a binary trie over the first 8 bytes of sha256 of the
// normalized owner/name: a node holding more than 100 repositories splits on
// its next hash bit, down to MaxDepth (32). A target with one shard keeps its
// base scope_id; a target with more names each leaf ScopeID(base, bits) with
// the full bit prefix, for example "<base>#shard-0110". The hash, the split
// rule, and the name format are durable identity (ingestion scopes, facts,
// and read states key on the id): changing any of them renames every shard
// scope and needs a migration.
//
// NormalizeRepository and NormalizeOrganization are the single owner/name
// normalization the three readers share. HasMarker lets configuration
// validation reject an operator scope_id that could collide with a generated
// shard id, and ParseScopeID recovers the base scope_id and bits of one.
//
// The package depends only on the standard library so the coordinator,
// collector, and query packages can import it without a cycle.
package shard
