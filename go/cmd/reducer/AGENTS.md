# AGENTS — cmd/reducer

This file guides LLM assistants working in `go/cmd/reducer`. Read it
before touching any file in this directory.

## Read first

1. `go/internal/reducer/README.md` and `go/internal/reducer/AGENTS.md` —
   all domain logic lives there, not here.
2. `docs/docs/deployment/service-runtimes.md` — Resolution Engine section.
3. `docs/docs/reference/nornicdb-tuning.md` — before touching any NornicDB
   env var or write path.
4. `CLAUDE.md` "Facts-First Bootstrap Ordering" and "Correlation Truth
   Gates" — understand Phase 1–4 before changing claim gating or domain
   ordering.
5. `CLAUDE.md` "Concurrency Workflow" — before changing worker counts,
   leases, retry delays, or batch sizes.

## Invariants (cite file:line)

- **Graph backend selection fails at startup for invalid values** —
  `main.go:162` calls `runtimecfg.LoadGraphBackend`; when the value is not
  `GraphBackendNornicDB` or the Neo4j equivalent, the error propagates to
  `os.Exit(1)`.
- **Projector drain gate is NornicDB + local-authoritative only** —
  `config.go:135–148` `loadReducerProjectorDrainGate` returns `true` only
  when the backend is `GraphBackendNornicDB` AND the query profile is
  `local-authoritative`.
- **Heartbeat renews at `LeaseDuration / 2`** — `main.go:353`
  `HeartbeatInterval: workQueue.LeaseDuration / 2`; do not set
  `ESHU_REDUCER_RETRY_DELAY` shorter than the lease TTL or claims will churn.
- **Prior-config depth defaults to 10; invalid input WARNs and falls back** —
  `PriorConfigDepth` is set from `parsePriorConfigDepth` at `main.go:294`.
  Invalid input (non-integer, negative) returns `0` and emits a WARN log via
  `slog` with `failure_class="env_parse"`. Empty input and explicit `"0"` both
  return `0` silently — they are documented sentinels for "use default", not
  operator errors. The loader resolves `0` (or any non-positive value) to
  `defaultPriorConfigDepth` (10). A `0` result keeps `removed_from_config`
  active — it does not disable it.
- **NornicDB batch claim size is `workers` (1:1)** — `config.go:75`
  returns `workers` when `GraphBackendNornicDB` is active;
  Neo4j default is `workers × 4` capped at 64.
- **NornicDB grouped writes are not promoted** — `main.go:158–168` logs a
  warning when `ESHU_NORNICDB_CANONICAL_GROUPED_WRITES=true`; this is
  conformance-testing only, not a production default.
- **Semantic-entity claim limit defaults to 1 on NornicDB** —
  `config.go:128–133`; override only with telemetry in view.

## Common changes

### Add a new env var

1. Add the constant to `config.go` alongside the existing `const` block.
2. Add a `load*` helper following the pattern of `loadReducerWorkerCount`
   or `loadDurationOrDefault`.
3. Wire the value into `buildReducerService` in `main.go`.
4. Update this README's configuration table and the service-runtimes doc.

### Change the drift prior-config depth

- `PriorConfigDepth` on the loader is set via `parsePriorConfigDepth`
  (`config.go:311`) from the `ESHU_DRIFT_PRIOR_CONFIG_DEPTH` env var
  (`main.go:294`). The package default lives in
  `go/internal/storage/postgres/tfstate_drift_evidence_prior_config.go` as
  `defaultPriorConfigDepth` (10). When changing the default, update both the
  constant and the README configuration table.

### Change worker count defaults

- Edit `loadReducerWorkerCount` in `config.go:150`; keep the
  NornicDB / Neo4j branches explicit.
- Capture before/after queue age and `eshu_dp_reducer_run_duration_seconds`
  before saying the change is correct.

### Add a new runner

1. Define the runner struct and config in `internal/reducer`.
2. Add config loading in `config.go`.
3. Wire the runner into `buildReducerService` in `main.go` after the
   existing runners.
4. Assign it to the `reducer.Service` struct literal.

### Change NornicDB write knobs

- Only touch `neo4j_wiring.go`.
- Document the change in `docs/docs/reference/nornicdb-tuning.md` and the
  active NornicDB ADR in the same PR.

## Failure modes

- **Queue claims churn**: `ESHU_REDUCER_RETRY_DELAY` set shorter than lease
  TTL causes failed intents to re-enter immediately; check
  `eshu_dp_queue_claim_duration_seconds` and `eshu_dp_reducer_executions_total`
  with status `failed`.
- **Projector drain gate deadlock** (NornicDB local-authoritative): the
  drain gate blocks semantic-entity claims until
  `ESHU_REDUCER_EXPECTED_SOURCE_LOCAL_PROJECTORS` projectors have published;
  if the projector count never reaches that threshold, the reducer blocks
  forever.
- **Grouped-write NornicDB regression**: `ESHU_NORNICDB_CANONICAL_GROUPED_WRITES=true`
  requires the same rollback, timeout, and no-partial-write invariants as
  Neo4j grouped writes. Enable it only for conformance runs, not production.
- **Startup failure on bad backend**: any value other than `neo4j` or
  `nornicdb` in ESHU_GRAPH_BACKEND causes immediate startup failure;
  operator intent is explicit.

## Anti-patterns

- Do not add `if graphBackend == nornicdb` branches outside `neo4j_wiring.go`.
  Backend dialect differences belong in documented narrow seams only.
- Do not change `buildReducerService` to accept a concrete Neo4j or
  NornicDB type; all graph writes go through capability ports.
- Do not add new environment variables without updating the README
  configuration table and service-runtimes docs.

## What NOT to change without an ADR

- Claim domain semantics (`ESHU_REDUCER_CLAIM_DOMAIN` behavior).
- The projector drain gate logic in `loadReducerProjectorDrainGate`.
- NornicDB grouped-write promotion from conformance to production default.
- The heartbeat interval formula (`LeaseDuration / 2`).
