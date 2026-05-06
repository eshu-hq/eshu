# Workflow Coordinator Implementation Gates

> **For agentic workers:** REQUIRED: Treat this document as a pre-implementation gate for the accepted workflow coordinator ADRs. Do not start production coordinator claim ownership until every gate in this file is resolved and linked to code-level tests.

**Goal:** Close the remaining implementation-shaping gaps after the workflow coordinator ADR and the concurrency companion ADR were accepted.

**Architecture:** The accepted ADRs define the ownership model and concurrency contract. This plan captures the remaining implementation-time decisions that must be pinned down before coordinator production claim ownership is enabled.

**Tech Stack:** Go, PostgreSQL, OpenTelemetry, MkDocs

---

## References

- [Workflow Coordinator And Multi-Collector Runtime Contract](/Users/allen/personal-repos/eshu/.worktrees/codex/go-data-plane-architecture/docs/docs/adrs/2026-04-20-workflow-coordinator-and-multi-collector-runtime-contract.md)
- [Workflow Coordinator Claiming, Fencing, And Convergence Contract](/Users/allen/personal-repos/eshu/.worktrees/codex/go-data-plane-architecture/docs/docs/adrs/2026-04-20-workflow-coordinator-claiming-fencing-and-convergence.md)

## Gate 1: Fencing Token Type And Issuance Semantics

**Question to close:** what exact token type and increment rule should the claim layer use?

**Decision target:**

- use a `BIGINT` fencing token per work item
- increment monotonically on every new claim epoch for the same work item
- require the token on heartbeat, completion, failure, and release paths

**Why this must be pinned down:**

- stale-owner rejection depends on numeric monotonicity
- completion checks and audit rows need one exact token format
- tests need deterministic expectations for rollover and stale completion

**Implementation notes:**

- persist the token in the durable claim row
- do not derive the token from timestamps
- do not use process-local counters

## Gate 2: Reaper Concurrency Contract

**Question to close:** how does the reaper avoid racing with another coordinator replica?

**Decision target:**

- reaper scans must themselves use a durable lock or bounded row-claiming
  contract
- two coordinator replicas must not expire and requeue the same claim twice
- the initial design should use `SELECT ... FOR UPDATE SKIP LOCKED`-style
  bounded reaper selection so multiple coordinator replicas cannot double-reap

**Why this must be pinned down:**

- expiry is part of ownership correctness, not a background convenience task
- duplicate reaping would create false retries and distorted backlog metrics

**Implementation notes:**

- use row-backed claim selection for reaping
- keep reaped rows auditable
- do not require singleton coordinator deployment for correctness

## Gate 3: Lease TTL And Heartbeat Defaults

**Question to close:** what are the initial timing defaults?

**Decision target:**

- initial default lease TTL: `60s`
- initial heartbeat cadence: `20s`
- require the heartbeat interval to remain comfortably below TTL

**Why this must be pinned down:**

- the claim system cannot be validated realistically without defaults
- observability thresholds need concrete expectations
- Compose and cloud validation need the same baseline assumptions

**Implementation notes:**

- make both values configurable
- validate that heartbeat interval cannot equal or exceed TTL
- expose both values through runtime status or config reporting

## Gate 4: Required Downstream Phases Per Slice

**Question to close:** how does the coordinator know which downstream reducer
phases are required before a slice may transition from `reducer_converging` to
`complete`?

**Decision target:**

- define one authoritative required-phase registry
- the registry may live in reducer domain wiring or a source-neutral phase
  policy package
- the coordinator reads that registry instead of hardcoding per-family guesses
- close this explicitly before schema PRs: the initial preference is a static
  registry keyed by bounded slice or collector family semantics, not ad hoc
  per-run phase declarations

**Why this must be pinned down:**

- false completeness is the exact bug class these ADRs are trying to prevent
- second-pass domains such as deployment mapping and workload materialization
  must be explicit, not implied

**Implementation notes:**

- map required phases by bounded slice type
- ensure the registry is testable
- do not infer required phases from incidental queue emptiness

## Gate 5: Webhook Back-Pressure Behavior

**Question to close:** what should `webhook-ingress` do when trigger volume
exceeds claim throughput?

**Decision target:**

- return `HTTP 429` or equivalent bounded rejection when run-request intake is
  above the safe threshold
- publish a shared saturation metric visible to operators
- keep webhook delivery handling idempotent on duplicate submissions
- wire both signals: immediate caller feedback via `HTTP 429` and operator
  visibility via shared queue-depth or saturation metrics

**Why this must be pinned down:**

- back-pressure is part of correctness under stress, not an optional nicety
- webhook flooding should degrade into visible bounded backlog, not claim thrash

**Implementation notes:**

- record dedup/replay outcomes separately from saturation rejections
- document expected client retry semantics
- surface webhook intake saturation in coordinator admin/status

## Gate 6: Workstream Extraction From ADR Appendix

**Question to close:** how do we keep the accepted ADRs stable while still
driving execution across Claude, Codex, and subagents?

**Decision target:**

- move detailed task slicing, ownership, and subagent dispatch into execution
  plans under `docs/superpowers/plans/`
- keep the ADR appendix stable and architecture-oriented

**Why this must be pinned down:**

- accepted ADRs should not churn every time task ownership changes
- execution plans should hold sprint and subagent specifics

**Implementation notes:**

- use the accepted ADRs as the architectural gate
- use dedicated execution plans for code tasks, test matrices, and owner split

## Suggested Next Planning Split

After these six gates are resolved, the next detailed implementation plan
should split into these execution tracks:

1. coordinator schema and claim tables
2. claim issuance, fencing, heartbeat, and reaper
3. downstream completeness registry and coordinator status computation
4. Git collector migration from `ingester` identity to `collector-git`
5. webhook ingress and trigger normalization
6. Compose, Helm, and `ops-qa` rollout validation
