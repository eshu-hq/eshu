<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# eshu-api destination content plan

Refs #8872, part of #6707. Status: preparation only. Nothing in this plan
has been pushed to the destination repository.

## Destination

`eshu-hq/eshu-api`: README-only at `a49341a`. It holds no cutover
authority until its reviewed cutover lands. Accountable
release/deployment owner: `@linuxdynasty` (owner-confirmed per the issue).
Deployment target: the owner-designated QA EKS environment.

## What moves (source: the ownership map)

`go/cmd/api/CUTOVER_OWNERSHIP.md` is the machine-checked census: 270
rows, 220 reads, 50 mutations, 63 handler families plus the mux level.

- Read families (all `channel=read` rows): move to `eshu-api` as consumers
  of versioned core read contracts. No durable writes travel with them.
- OpenAPI contract source (`go/internal/query/openapi/paths/`,
  `components*.go`, `schema/`, `spec.go` assembly order): moves as the
  published contract. The spec is the cutover's compatibility surface; the
  destination owns its fragments after the move.
- Mutation HTTP shapes (all 50 `channel=mutation` rows): the routes move,
  the stores do not. Each persists through an explicit versioned core
  admission contract (recovery, retirement, terminal-failure ack, identity,
  provider-config, sign-in-policy, session, bootstrap, login-state, and the
  single vulnerability-suppression mutation store).
- `wireAPI`-mounted families (26 rows: setup, OIDC/SAML/GitHub login,
  providers, provider configs, sign-in policy, ask, service intelligence
  report) move with their keyring/resolver composition documented as
  required construction inputs, not hidden globals.

## What stays in the core

Fact admission, canonical identity, projection, retraction, persistence,
recovery, graph truth, queue/lease state, schema bootstrap, and one-shot
repair (`go/internal/storage/postgres`, reducer, graph writers, admission
stores). Sharing a Postgres cluster during migration does not promote its
tables to a public API.

## Proposed destination layout (not created)

- `api/` — HTTP server, router, auth middleware, v1 alias and deprecation
  middleware (behavior pinned by the in-tree middleware tests).
- `contracts/` — the moved OpenAPI fragments plus a compatibility policy
  naming the supported core versions.
- `readclient/` — versioned core read-contract clients (no core
  implementation handles, no shared-table shortcuts).
- `admission/` — versioned mutation admission contracts, one per
  mutation family above.
- `deploy/` — independent build, release, and QA EKS deployment
  configuration; compatibility matrix against supported core versions.

## Parity and operations proof (acceptance criteria, mostly open)

Representative and hostile fixtures, auth/authz, pagination, truth/error
envelopes, and OpenAPI agreement are proven per family by the existing
in-tree suite (cited per family in review). Still open and required before
any cutover: extracted-path parity on QA EKS, concurrent
retry/duplicate-delivery/ordering/cancellation proof, resource-bound and
no-regression measurements, health/readiness/freshness signals, credential
handling and provenance, supported version pairs, independent build/release,
rollback rehearsal, and independent review.

## PR plan (no push to the destination from this lane)

1. This lane's `eshu` PR: the ownership map, the locking contract tests,
   and this plan. `Refs #8872`, never a closing keyword.
2. Destination seeding PR (separate, reviewed): proposed layout plus the
   moved contract source, reviewed against the map row-for-row.
3. QA EKS deployment and parity/operations proof, recorded as evidence on
   #8872 with the tested artifact, topology, and evidence location.
4. Cutover only after green gates, independent review, and a rehearsed
   rollback. The in-tree implementation is removed after production
   acceptance, not before.
