// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package secrets implements the secrets/IAM read routes behind
// /api/v0/secrets-iam/* (#6642, Part A of the query-root family moves):
// Handler, its Mount method, the five list/summary route handlers, the
// Postgres-backed identity-trust-chain, privilege-posture-observation,
// secret-access-path, and posture-gap stores, the Postgres-backed posture
// summary rollup, and the graph-backed S3 external-principal grant-posture
// store.
//
// Handler gates every read on its own per-route capability
// (querycontract.CapabilityUnsupported) before touching a store, requires a
// scope or entity anchor before issuing any query, and enforces the caller's
// scoped-token grant (querycontract.RepositoryAccessFilterFromContext) before
// dispatching to a store. Every list route over-fetches its bounded limit by
// one to report truncation and a next_cursor deterministically. Rows carry
// fingerprints, join keys, states, and evidence IDs only -- no secret value,
// raw IAM role ARN, ServiceAccount name, Vault role name, or path crosses the
// wire.
//
// This package imports querycontract (profiles, envelopes, capability
// registration, HTTP helpers, read ports, and repository-access filtering)
// and queryspan (the shared handler-span seam); it MUST NOT import the query
// root, or root would cycle back through its own compatibility aliases in
// secrets_alias.go, which import this package for the Handler type alias and
// the six Postgres/Graph store constructor forwarders cmd/api and
// cmd/mcp-server wiring still use. See README.md for the file layout and move
// evidence, and AGENTS.md for the per-symbol export rationale.
package secrets
