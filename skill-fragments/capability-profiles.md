---
id: capability-profiles
version: 1.0.0
byte_citation: docs/public/reference/capability-conformance-spec.md#29-49
description: |
  Profiles are local_lightweight, local_authoritative,
  local_full_stack, or production. A profile row marked unsupported
  MUST return `unsupported_capability`. Truth ceilings live in
  specs/capability-matrix.v1.yaml and go/internal/query/contract.go.
---

# Eshu Capability Profiles

Every Eshu runtime runs under exactly one capability profile. The
profile determines which capabilities are supported, what their
authoritative truth level is, and which MCP/API surface is exposed.

The four profiles are:

- `local_lightweight` — code-only discovery, in-memory graph, no
  collector fan-out. Used for fast local iteration.
- `local_authoritative` — code plus local relationships (Terraform,
  Helm, Kustomize, Argo). Used for local-host deployment tracing.
- `local_full_stack` — code plus relationships plus a live graph
  backend (NornicDB default; Neo4j compatibility only when it
  satisfies the shared Cypher/Bolt contract). Used for full local
  proof runs.
- `production` — the deployed Eshu service. Same surface as
  `local_full_stack` plus collector queues, Postgres, and the
  hosted status surface.

A profile row marked `unsupported` MUST return
`unsupported_capability` from MCP and API surfaces. The truth ceiling
for a capability (the maximum `truth` level the profile can return)
lives in `specs/capability-matrix.v1.yaml` and is enforced by
`go/internal/query/contract.go`.
