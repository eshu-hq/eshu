# Local Eshu Service Performance Envelope

This page defines the target envelope for the local Eshu service. Local Eshu
should be useful on a normal developer laptop, not only on ideal hardware or
empty repositories.

## Reference Hardware

- Apple Silicon laptop with 16 GB RAM
- or mid-range x86 laptop with 4+ cores and 16 GB RAM

Targets are split by profile because `local_authoritative` starts the managed
graph backend and carries more cold-start cost.

## `local_lightweight`

Eshu plus embedded Postgres only.

- cold start: under 5 seconds
- warm restart: under 2 seconds
- exact symbol lookup p95: under 500 ms
- content search p95: under 800 ms
- complexity query p95: under 1500 ms
- single-file reindex to visible search update: under 2 seconds
- initial index of an active repo: measured against capability matrix scope
  sizes
- idle memory budget: measured and recorded
- active indexing memory budget: measured and recorded

## `local_authoritative`

Eshu plus embedded Postgres and embedded NornicDB.

- cold start: under 15 seconds
- warm restart: under 5 seconds with the same workspace data root
- transitive caller p95: under 2 seconds on an active repo
- call-chain path p95: under 2 seconds on an active repo
- dead-code scan for an active repo: under 10 seconds
- reducer bulk write batch: under 10 seconds for 50K facts
- idle memory budget: measured for Eshu host plus graph backend
- active indexing memory budget: measured for Eshu host plus graph backend
- single-file reindex to visible transitive-caller update: under 5 seconds

Warm restart means the same workspace data root is reused and no full reindex is
required. Cold start means starting from stopped processes.

## Dogfood Tiers

Use these tiers when recording local-authoritative indexing and dead-code
evidence. Tiers are comparison buckets, not excuses to keep slow paths.

| Tier | Shape | Typical use |
| --- | --- | --- |
| 0 | Synthetic fixtures and package tests | Prove handler, parser, graph, and query contracts. |
| 1 | Active repo under about `5K` files or `50K` entities | Normal local developer proof. |
| 2 | Large repo under about `25K` files or `300K` entities | Language dogfood before promotion. |
| 3 | Stress repo over about `25K` files or `300K` entities | Backend and projection pressure tests. |
| 4 | Multi-repo corpus | Scheduling, queue, and memory pressure. |

A dogfood note must include tier, commit or branch, repository name, language
focus, file count, entity count, fact count, terminal state, and stage durations
for collector stream, fact commit, projector fact load, canonical write,
reducer domains, shared projections, and query latency.

## Durable Performance Rules

Apply these rules to every repo-scale performance claim.

- Apply schema first. For Compose, Kubernetes, and backend comparisons, run the
  data-plane schema bootstrap before indexing.
- Record collector stream complete, projection/bootstrap complete, and
  queue-zero separately.
- Walk the proof ladder in order: focused fixture, single repo, representative
  medium subset, then full corpus.
- Capture backend identity: Eshu commit, NornicDB or Neo4j image tag or commit,
  clean-volume state, schema state, runtime knobs, queue counts, retry counts,
  and dead letters.
- Treat timeouts as symptoms. Classify query shape, missing schema/index,
  backend fallback, transaction validation, queue behavior, stale images,
  background backend work, and real timeout-budget misses before changing the
  timeout.
- Do not ship serialization as a performance fix. Lowering worker counts is a
  baseline, temporary safeguard, or proven permanent serial contract only.
- Keep performance and observability evidence in versioned docs. PR text alone
  is not enough.

For hot-path PRs, the evidence note must use the markers consumed by
`scripts/verify-performance-evidence.sh`: `Performance Evidence:`,
`Benchmark Evidence:`, or `No-Regression Evidence:` plus either
`Observability Evidence:` or `No-Observability-Change:`.

Tier 3 and Tier 4 runs may exceed the Tier 1 local target only when stage
evidence explains why. If one write shape or reducer domain dominates, the next
action should target that owner instead of averaging the result away.

## Backpressure Expectations

- fsnotify events must be coalesced and debounced
- parse and projection pools must be bounded
- the runtime must prefer bounded lag over unbounded CPU or memory growth

## Manual Perf Gates

Startup envelope:

```bash
ESHU_LOCAL_AUTHORITATIVE_PERF=true \
  go test -tags nolocalllm ./cmd/eshu -run TestLocalAuthoritativeStartupEnvelope -count=1 -v
```

Synthetic call-chain envelope:

```bash
ESHU_LOCAL_AUTHORITATIVE_PERF=true \
  go test -tags nolocalllm ./cmd/eshu -run TestLocalAuthoritativeCallChainSyntheticEnvelope -count=1 -v
```

Synthetic transitive-caller envelope:

```bash
ESHU_LOCAL_AUTHORITATIVE_PERF=true \
  go test -tags nolocalllm ./cmd/eshu -run TestLocalAuthoritativeTransitiveCallersSyntheticEnvelope -count=1 -v
```

Synthetic dead-code envelope:

```bash
ESHU_LOCAL_AUTHORITATIVE_PERF=true \
  go test -tags nolocalllm ./cmd/eshu -run TestLocalAuthoritativeDeadCodeSyntheticEnvelope -count=1 -v
```

These gates prove the local-authoritative startup and synthetic query paths.
They are not substitutes for active-repo transitive-caller, active-repo
call-chain, active-repo dead-code, reducer-throughput, memory-budget, or
full-corpus drain evidence.

## Open Evidence

These targets remain open until their own accepted perf gates land:

- active-repo dead-code scan
- reducer bulk write throughput for 50K facts
- idle and active memory budgets for Eshu host plus graph backend
- active-repo transitive-caller and call-chain latency
- full-corpus `local_authoritative` drain gate with terminal queue-zero state

## Review Rule

If local Eshu misses these targets, update the docs and capability matrix to
show the actual supported envelope. Do not hide the miss behind stale evidence.
