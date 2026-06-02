# Local Eshu Service Performance Envelope

This page defines the expected local performance envelope. Local Eshu should be
useful on a normal developer laptop, not only on ideal hardware or empty
repositories.

Reference hardware is an Apple Silicon or mid-range x86 laptop with at least
4 cores and 16 GB RAM.

## Target Profiles

| Profile | Runtime shape | Target |
| --- | --- | --- |
| `local_lightweight` | Eshu plus embedded Postgres | cold start under `5s`; warm restart under `2s`; exact symbol lookup p95 under `500ms`; content search p95 under `800ms`; complexity query p95 under `1500ms`; single-file reindex to visible search update under `2s`. |
| `local_authoritative` | Eshu plus embedded Postgres and embedded NornicDB | cold start under `15s`; warm restart under `5s`; transitive caller and call-chain p95 under `2s` on an active repo; active-repo dead-code scan under `10s`; reducer bulk write batch under `10s` for `50K` facts; single-file reindex to visible graph update under `5s`. |

Warm restart means the same workspace data root is reused and no full reindex is
required. Cold start means starting from stopped processes.

Memory budgets must be measured for each profile. `local_authoritative`
measurements include the Eshu host process plus graph backend.

## Dogfood Tiers

| Tier | Shape | Use |
| --- | --- | --- |
| 0 | Synthetic fixtures and package tests | Handler, parser, graph, and query contracts. |
| 1 | Active repo under about `5K` files or `50K` entities | Normal local developer proof. |
| 2 | Large repo under about `25K` files or `300K` entities | Language dogfood before promotion. |
| 3 | Stress repo over about `25K` files or `300K` entities | Backend and projection pressure tests. |
| 4 | Multi-repo corpus | Scheduling, queue, and memory pressure. |

A dogfood note must include tier, commit or branch, repository name, language
focus, file count, entity count, fact count, terminal state, stage durations,
backend, runtime knobs, retry counts, and dead letters.

## Evidence Rules

- Apply schema before indexing for Compose, Kubernetes, and backend comparisons.
- Record collector stream complete, projection or bootstrap complete, and
  queue-zero separately.
- Walk the proof ladder in order: focused fixture, single repo,
  representative medium subset, then full corpus.
- Treat timeouts as symptoms. Classify query shape, missing schema/index,
  backend fallback, transaction validation, queue behavior, stale images,
  background backend work, and real timeout-budget misses before changing a
  timeout.
- Do not ship serialization as a performance fix. Worker-count reductions,
  single-threaded drains, disabled concurrent writers, or batch size `1` are
  diagnostics unless the serial path is the proven permanent contract.
- Keep performance and observability evidence in versioned repo files. PR text
  alone is not proof.

Hot-path PRs must use one performance marker consumed by
`scripts/verify-performance-evidence.sh`:

- `Performance Evidence:`
- `Benchmark Evidence:`
- `No-Regression Evidence:`

They must also use either `Observability Evidence:` or
`No-Observability-Change:`.

## Current Hot-Path Evidence

### EC2 Block-Device KMS Posture Writer (#1304)

Benchmark Evidence: `go test ./internal/storage/cypher -run '^$' -bench
BenchmarkEC2BlockDeviceKMSPostureNodeWriter -benchmem -count=3` on darwin/arm64
Apple M4 Pro writes 5,000 uid-anchored EC2 posture property rows at
2.43-2.45ms/op, 3.61MB/op, and 35,068 allocs/op with a no-op group executor,
isolating Eshu-owned statement construction and batching from graph round trips.
The writer uses one batched `UNWIND` + `MATCH (resource:CloudResource {uid:
row.uid})` + `SET` shape and never performs per-volume graph lookups.

Observability Evidence: `reducer.ec2_block_device_kms_posture_materialization`
wraps fact load, dual readiness, extraction, retract, and graph write. The
handler emits `eshu_dp_ec2_block_device_kms_posture_decisions_total` by
`outcome`/`reason`, `eshu_dp_ec2_block_device_kms_posture_skipped_total` by
`skip_reason`, and a completion log with resource/relationship/posture counts,
row count, decision and skip tallies, and stage durations.

## Manual Gates

```bash
ESHU_LOCAL_AUTHORITATIVE_PERF=true \
  go test -tags nolocalllm ./cmd/eshu -run TestLocalAuthoritativeStartupEnvelope -count=1 -v

ESHU_LOCAL_AUTHORITATIVE_PERF=true \
  go test -tags nolocalllm ./cmd/eshu -run TestLocalAuthoritativeCallChainSyntheticEnvelope -count=1 -v

ESHU_LOCAL_AUTHORITATIVE_PERF=true \
  go test -tags nolocalllm ./cmd/eshu -run TestLocalAuthoritativeTransitiveCallersSyntheticEnvelope -count=1 -v

ESHU_LOCAL_AUTHORITATIVE_PERF=true \
  go test -tags nolocalllm ./cmd/eshu -run TestLocalAuthoritativeDeadCodeSyntheticEnvelope -count=1 -v
```

These gates prove startup and synthetic query paths. They are not substitutes
for active-repo transitive-caller, call-chain, dead-code, reducer-throughput,
memory-budget, or full-corpus drain evidence.

## Open Evidence

These targets remain open until accepted perf gates land:

- active-repo dead-code scan
- reducer bulk write throughput for `50K` facts
- idle and active memory budgets for Eshu host plus graph backend
- active-repo transitive-caller and call-chain latency
- full-corpus `local_authoritative` drain with terminal queue-zero state

If local Eshu misses these targets, update the docs and capability matrix to
show the actual supported envelope. Do not hide the miss behind stale evidence.
