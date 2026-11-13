# AGENTS.md — internal/oci/registry guidance

## Read first

1. `README.md` for ownership and invariants.
2. `identity.go` for the provider vocabulary and normalization rules.
3. `../coordinator/oci/registry/planner.go` for the only production
   consumer and its per-provider dispatch.

## Invariants

- Keep provider calls, credential resolution, and distribution clients out.
  This package parses configured values into identities; it never opens a
  registry connection.
- Keep every rejection error free of credentials. URL helpers must strip
  userinfo before failing; the credentialed-URL tests pin this.
- Preserve exact error strings, normalized spellings, and stable
  `oci-registry://` ID grammar: planner tests and scope IDs depend on them.
- Do not reintroduce descriptor or envelope types here. Descriptor identity
  moved with the satellites; this package covers repository identity only.

## Common changes

Write a failing package test before changing normalization, a provider
spelling rule, or an endpoint parser. A new provider needs a
`*RepositoryIdentity` constructor, a `Provider*` constant, a planner
`case`, and planner coverage mirroring
`TestOCIRegistryWorkPlannerNormalizesProviderEndpointFields`.

## Verification

Run the package tests, then the coordinator planner tests
(`../coordinator/oci/registry`), then build the whole module: the planner
is the only importer, so a signature change breaks it first.
