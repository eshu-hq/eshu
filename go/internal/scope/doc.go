// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package scope defines durable identity and generation lifecycle for source
// scopes ingested by Eshu collectors.
//
// IngestionScope captures the bounded source-local identity (repository,
// account, region, cluster, state snapshot, documentation source, container
// registry repository, PagerDuty account, or event trigger). ScopeGeneration
// captures one observed snapshot and tracks the pending -> active ->
// (superseded | completed | failed) lifecycle through an explicit transition
// table. Validation rejects unknown statuses, blank identifiers, zero
// timestamps, and forbidden transitions. Scanner-worker scopes represent
// bounded security analyzer work and keep resource-heavy scanning separate from
// reducer-owned truth. Semantic-extraction collector identity marks optional
// model-assisted provenance and must not be treated as deterministic graph
// truth or carry provider keys, raw prompts, or provider responses in scope
// identifiers. Terraform state helpers create stable state-snapshot scopes from
// backend kind plus locator hash, while generation identity carries state serial
// and lineage so serial changes do not rewrite the scope boundary.
// The state-snapshot scope hash MUST agree with terraformstate.ScopeLocatorHash
// byte-for-byte; the drift resolver join breaks if the two diverge (issue
// #203).
// AllCollectorKinds enumerates every collector kind in a stable order and is the
// single source of truth for tooling that must cover the full collector fleet
// (readiness reports, promotion proofs, fleet hygiene); adding a collector means
// updating that one list.
package scope
