<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2026 eshu-hq -->

# Removal-tail PREP: retired-coverage honesty-note skeleton (#9669)

Refs #9669, part of #6707. Status: skeleton only — section headings and
TBD markers for the honesty note the removal PR must carry per issue
acceptance ("Retired-coverage honesty note in the removal PR (what the
satellite does NOT cover, if anything)") and arbiter §6.3. No proof is
invented here; every substantive claim below is TBD until the named Leaf
B–E evidence lands. Copy this skeleton into the removal PR body and
replace each TBD with measured evidence or an explicit non-coverage
statement.

## 1. Destination telemetry state — TBD (Leaf E proof pending)

In-tree signals that must have a named satellite equivalent or an
explicit gap admission:

- `eshu_dp_neo4j_query_duration_seconds` and
  `eshu_dp_postgres_query_duration_seconds` (`/metrics`; see
  `go/cmd/api/AGENTS.md` failure-modes notes).
- `eshu_dp_shutdown_duration_seconds` histogram on the shutdown path.
- The `otelhttp` span named `eshu-api` and the `runtime.startup.failed`
  structured-log event.
- TBD: satellite metric/span/log names, retention parity, and the
  dashboard/alert updates that moved with them.

## 2. OpenAPI fragment ownership — TBD (Leaf B proof pending)

- 137 `.go` files under `go/internal/query/openapi/paths/` (18 of them
  package `doc.go` files; 36 adjacent md files not counted — measured
  in this worktree; file set identical at the lane base `60f2fe256d`
  and at `e30c99c1cf` — no drift since the base), assembled into the
  served spec and drift-gated by
  `apicontract_drift_test` plus the operation-id convention test.
- Landed since the base (dest-side progress, ownership decision still
  TBD): the satellite now carries ported fragments for admin-identity
  mutations (#42) and reads (#43, relay + union), the
  AdminProviderConfigMutations subset (#46), the AdminRecoveryMutations
  subset (#47), and the Setup carve-out routes (#45, fragment posture
  still open under satellite #44).
- TBD: which repo owns spec assembly post-cutover, where the drift gate
  runs, and what happens to the in-tree fragment tree (moved verbatim,
  vendored, or deleted with the satellite as sole owner).

## 3. Relay-only-proven routes — TBD (Leaves B–D proof pending)

- Candidate set: pure-`read` rows served by handlers holding a writer
  store — Status (18 routes) and SupplyChain (19 routes, 18 reads sharing
  the suppression handler) — which dispatch/proxy to core until the read
  side separates from the writer handle.
- Landed since the base: the Leaf B read-transport spec plus red-first
  conformance skeleton merged in-tree (#10250); the cross-repo harness
  itself has not run, so no parity evidence exists yet.
- TBD: per-route native vs dispatch disposition at removal time, and for
  each relay-only route the parity evidence (envelope negotiation,
  pagination, capability/profile gating, authz parity) the Leaf B
  cross-repo harness recorded.

## 4. Single version pair — TBD (Leaf B confirmation pending)

- `CoreReadContractsVersion = "v0"`
  (`go/internal/query/querycontract/api_boundary.go:16`, pinned by
  `api_boundary_test.go` — re-verified still `v0` at `e30c99c1cf`); the Postgres `ReadStore` half pinned by
  `TestReadStoreContractIsVersioned`.
- `SuppressionAdmission` at `SuppressionAdmissionContractVersion`, bound
  by `newSupplyChainHandler` to the core-owned Postgres store — the single
  bounded mutation survivor.
- Landed since the base (new versioned seams, still unwired):
  `RecoveryAdmission` at `RecoveryAdmissionContractVersion = "v0"`
  (#10245, `go/internal/query/admin/recovery_admission.go`) and the
  `GovernanceAuditAdmission` append seam (#10233,
  `go/internal/query/governance_audit_admission.go`).
- TBD: Leaf B confirmation that no port change forced a version bump
  before removal, and the satellite-pinned version pair recorded in the
  removal PR.

## 5. In-tree gate coverage satellite CI lacks — TBD (Leaf B/E proof pending)

- `read-api-latency-gate.yml` route budgets
  (`testdata/benchmarks/read-api-route-budgets.txt`,
  `read-api-route-work-budgets.txt`): TBD whether the eshu-api repo runs
  the same budgets against a pinned core image.
- Golden-corpus and replay-coverage gates: TBD whether the Leaf B
  cross-repo parity harness replays them against an eshu-api deployment
  with named corpus revisions.
- Capability-matrix probes (`go/cmd/capability-inventory`): TBD whether
  probes run against the satellite in satellite CI.
- Any gate above with no satellite equivalent must be named here as
  retired coverage, with its owner and follow-up issue — never silently
  dropped.
