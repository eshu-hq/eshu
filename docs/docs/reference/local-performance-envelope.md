# Local Eshu Service Performance Envelope

This document defines the target envelope for the local Eshu service.

## Goals

The local Eshu service should be useful on a normal developer laptop, not only
on ideal hardware or empty repos.

## Initial Targets

Reference hardware:

- Apple Silicon laptop with 16 GB RAM
- or mid-range x86 laptop with 4+ cores and 16 GB RAM

Targets are split by profile because the `local_authoritative` profile starts
NornicDB and therefore carries more cold-start cost.

### `local_lightweight` profile (Eshu + embedded Postgres only)

- cold start: under 5 seconds
- warm restart: under 2 seconds
- exact symbol lookup p95: under 500 ms
- content search p95: under 800 ms
- complexity query p95: under 1500 ms
- single-file reindex to visible search update: under 2 seconds
- initial index of an active repo: document and measure against the capability
  matrix scope sizes
- idle memory budget: document and measure
- active indexing memory budget: document and measure

### `local_authoritative` profile (Eshu + embedded Postgres + embedded NornicDB)

- cold start: under 15 seconds (Postgres warmup plus graph-backend warmup)
- warm restart: under 5 seconds (same workspace data root, graph backend
  data directory reused)
- transitive caller p95: under 2 seconds on an active repo
- call-chain path p95: under 2 seconds on an active repo
- dead-code scan for an active repo: under 10 seconds
- reducer bulk write batch: under 10 seconds for 50K facts
- idle memory budget: document and measure (Eshu host + graph backend idle)
- active indexing memory budget: document and measure (Eshu host + graph
  backend under load)
- single-file reindex to visible transitive-caller update: under 5 seconds

## Workload Shapes

Targets should be tracked at least for:

- active repo
- active monofolder

The capability matrix should tie latency expectations to these scope sizes.

Warm restart means the same workspace data root is reused and no full reindex is
required. Cold start means starting the host from a stopped state with no warm
processes already attached.

## Dogfood Tiers

Use these tiers when recording local-authoritative indexing and dead-code
evidence. The tiers are comparison buckets, not excuses to keep slow paths.
Every tier still follows accuracy first, then performance.

| Tier | Shape | Typical use | Initial expectation |
| --- | --- | --- | --- |
| 0 | Synthetic fixtures and package tests | Prove handler, parser, graph, and query contracts | milliseconds to seconds |
| 1 | Active repo under about `5K` files or `50K` entities | Normal local developer proof | end to end near or under `1m` |
| 2 | Large repo under about `25K` files or `300K` entities | Language dogfood before promotion | measured, explain any stage over `1m` |
| 3 | Stress repo over about `25K` files or `300K` entities | Backend and projection pressure tests | measured separately from Tier 1 targets |
| 4 | Multi-repo corpus | Scheduling, queue, and memory pressure | measured with drained terminal state |

A dogfood note must include the tier, commit or branch, repository name,
language focus, file count, entity count, fact count, terminal state, and stage
durations for collector stream, fact commit, projector fact load, canonical
write, reducer domains, shared projections, and dead-code query latency.

For hot-path PRs, the evidence note must use the markers consumed by
`scripts/verify-performance-evidence.sh`: `Performance Evidence:`,
`Benchmark Evidence:`, or `No-Regression Evidence:` plus either
`Observability Evidence:` or `No-Observability-Change:`. This keeps the
benchmark and operator-signal proof in versioned docs where future agents can
find it.

Tier 3 and Tier 4 runs are allowed to exceed the Tier 1 one-minute local target,
but only with stage evidence that explains why. If a Tier 3 run spends most of
its time in one write shape or one reducer domain, the next action should target
that owner instead of averaging the result away.

Current Tier 3 stress evidence:

- Elasticsearch at commit `b8b07a60c0eb100c53120d6d2fa060f105d174a9` was
  indexed from a disposable local worktree on 2026-05-08 local time with
  NornicDB `v1.0.44`. The collector discovered `32966` files, parsed `32793`
  files, emitted `1093371` content entities, and upserted `1153024` facts.
- In the clean rerun, collector stream completed in `37.971s`, fact upsert in
  `73.576s`, projector fact load in `22.329s`, canonical phase-group write in
  `236.720s`, and source-local projection in `399.154s`.
- Reducer domains observed before the run was stopped for analysis:
  deployable-unit correlation `10.490s`, deployment mapping `11.258s`,
  workload identity `0.006s`, workload materialization `21.483s`, and code-call
  materialization `117.932s`.
- This run classifies Elasticsearch as a Tier 3 stress repo, not a Tier 1 local
  baseline. The next performance owner is canonical entity projection/write
  shape, followed by code-call materialization.

## Backpressure Expectations

- fsnotify events must be coalesced and debounced
- parse and projection pools must be bounded
- the runtime must prefer bounded lag over unbounded CPU or memory growth

## Current Startup Evidence

The `local_authoritative` startup envelope now has a dedicated manual gate:

```bash
ESHU_LOCAL_AUTHORITATIVE_PERF=true \
  go test -tags nolocalllm ./cmd/eshu -run TestLocalAuthoritativeStartupEnvelope -count=1 -v
```

That gate boots the real local Eshu service, embedded Postgres, schema bootstrap, and
embedded NornicDB, then measures readiness at the owner-record and
ingester handoff. It runs twice against the same workspace data root so the
first pass captures cold start and the second pass captures warm restart.

Recorded sample on 2026-04-23:

- cold start: `9.045253708s`
- warm restart: `490.996625ms`

These measurements pass the current `local_authoritative` startup targets.
Broader dead-code, reducer-throughput, and memory-budget targets remain open
until their own perf gates land.

## Current Query-Latency Evidence

The `local_authoritative` call-chain path now has a dedicated manual smoke:

```bash
ESHU_LOCAL_AUTHORITATIVE_PERF=true \
  go test -tags nolocalllm ./cmd/eshu -run TestLocalAuthoritativeCallChainSyntheticEnvelope -count=1 -v
```

That gate boots the real local Eshu service, embedded Postgres, and embedded NornicDB,
seeds a synthetic four-function `CALLS` chain through the shared Bolt
driver path, and exercises the real `/api/v0/code/call-chain` handler in
`local_authoritative`.

Recorded sample on 2026-04-23:

- synthetic call-chain p95: `736.25µs`

This smoke confirms that the backend-routed NornicDB call-chain path is
functionally live and comfortably below the current `under 2 seconds` target
for a synthetic path workload. It is not a substitute for active-repo
transitive-caller, active-repo call-chain, dead-code, or memory-budget proof.

The `local_authoritative` transitive-caller path now also has a dedicated
manual smoke:

```bash
ESHU_LOCAL_AUTHORITATIVE_PERF=true \
  go test -tags nolocalllm ./cmd/eshu -run TestLocalAuthoritativeTransitiveCallersSyntheticEnvelope -count=1 -v
```

That gate boots the real local Eshu service, embedded Postgres, and embedded NornicDB,
seeds the same synthetic four-function `CALLS` chain through the
shared Bolt driver path, and exercises the real
`/api/v0/code/relationships` transitive-callers handler in
`local_authoritative`.

Recorded sample on 2026-04-23:

- synthetic transitive-caller p95: `1.917916ms`

This smoke confirms that the backend-routed NornicDB transitive-callers path
is functionally live and comfortably below the current `under 2 seconds`
target for a synthetic traversal workload. It is still not a substitute for
active-repo transitive-caller, dead-code, or memory-budget proof.

The `local_authoritative` dead-code path now also has a dedicated manual
smoke:

```bash
ESHU_LOCAL_AUTHORITATIVE_PERF=true \
  go test -tags nolocalllm ./cmd/eshu -run TestLocalAuthoritativeDeadCodeSyntheticEnvelope -count=1 -v
```

That gate boots the real local Eshu service, embedded Postgres, and embedded NornicDB,
seeds a synthetic repository/file/function containment graph plus one
live `CALLS` edge through the shared Bolt driver path, and exercises the real
`/api/v0/code/dead-code` handler in `local_authoritative`.

Recorded sample on 2026-04-23:

- synthetic dead-code p95: `3.174125ms`

This smoke confirms that the backend-routed NornicDB dead-code candidate query
and derived-policy filter path are functionally live and comfortably below the
current `under 10 seconds` target for a synthetic workload. It is still not a
substitute for active-repo dead-code, reducer-throughput, or memory-budget
proof.

## Pending Perf Gates

The following targets remain open until their own perf gates land:

- **Active-repo dead-code scan** — target `under 10 seconds` (see
  `local_authoritative` targets). The synthetic
  `TestLocalAuthoritativeDeadCodeSyntheticEnvelope` gate now exists and proves
  the handler path is live, but active-repo numbers are still required before
  the `code_quality.dead_code` capability can claim anything stronger than the
  current derived truth contract.
- **Reducer bulk write throughput** — target `under 10 seconds` for 50K
  facts.
- **Idle and active memory budgets** for the combined Eshu host + graph
  backend footprint.
- **Full-corpus `local_authoritative` drain gate** — latest accepted remote
  run drained `896` repositories and `8347` fact queue rows in `14m13.6s`,
  with `344148` `code_calls` shared-projection rows drained and `0` pending,
  in-flight, retrying, failed, or dead-letter rows. Local developer runs now
  size snapshot, parse, projector, and NornicDB reducer workers from host CPU
  count unless explicit env vars are present.
- **Active-repo transitive-caller and active-repo call-chain** — current
  evidence is synthetic only; active-repo numbers are required before
  promoting the matrix entries past `derived`.

## Review Rule

If the local Eshu service misses these targets, the docs and matrix should reflect the
actual supported envelope instead of hiding the miss.
