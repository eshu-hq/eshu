# Reducer Agent Rules

These rules are mandatory for changes under `go/internal/reducer`.

## Read First

1. `README.md` and `doc.go`.
2. `docs/public/architecture.md`
3. `docs/public/deployment/service-runtimes.md`
4. `docs/public/reference/telemetry/index.md`
5. `go/internal/projector/README.md`
6. `go/cmd/reducer/README.md`

## Invariants

- Reducer domains MUST be cross-source, cross-scope, and truth-emitting through
  durable canonical writes or bounded counter emission.
- Intent lifecycle is fixed: pending, claimed, running, succeeded, failed.
- Generation supersession is a write barrier. Stale intents MUST NOT write
  graph rows or reducer facts.
- Heartbeat MUST stop before Ack so a committed item cannot keep extending its
  lease.
- Domains that consume `resolved_relationships` MUST have a post-Phase-3 reopen
  or re-trigger after deployment mapping reopens.
- Graph writes and phase publication are not atomic. Keep
  `GraphProjectionPhaseRepairQueue` and `GraphProjectionPhaseRepairer` wired.
- Shared projection intent IDs MUST stay stable SHA256 identities.
- Shared projection domains MUST gate on the required readiness phase.
- SQL trigger functions MUST keep trigger-to-`SqlFunction` `EXECUTES`
  reachability for dead-code correctness.
- Graph writes MUST go through `internal/storage/cypher`; no direct driver
  calls from domain handlers.
- JavaScript dynamic-call alias parsing MUST stay indexed once per function and
  cache negative scans.

## Change Rules

- New reducer domain: add the domain constant, handler, default wiring, truth
  contract, command adapters, telemetry, failing test first, and post-Phase-3
  reopen when it consumes `resolved_relationships`.
- Queue claim, Ack, Fail, retry, or lease change: treat it as a concurrency
  change. Map shared state, idempotency, lock/claim ordering, retry boundaries,
  and dead-letter behavior before editing.
- New graph projection phase: add the phase constant, verify keyspace and
  readiness gating, update Postgres schema when needed, and add tests.
- Shared projection runner config change: update runner config, command
  defaults, README, and public docs if operators see the knob.

## Failure Checks

- `deployment_mapping` stuck: verify Phase 3 reopen ran and readiness rows
  exist.
- Shared projection skipped: inspect readiness rows for the required phase.
- Repair queue growth: inspect phase publisher Postgres health and repair logs.
- Supersession flood: inspect ingester generation rate and reducer backlog.
- Heartbeat lease failure: inspect slow graph writes or Postgres saturation.
- Slow code-call extraction: benchmark large JavaScript dynamic-call handling
  before changing graph or queue code.

## Forbidden Without Architecture-Owner Approval

- Deployment mapping Phase 3 reopen requirement.
- Domain `OwnershipShape` invariants.
- Heartbeat, lease, retry, Ack, or Fail contract.
- Shared projection identity fields.
- Graph projection phase repair contract.
- Readiness phase ordering.
- Backend-specific domain logic.
- Worker-count serialization as a correctness fix.
