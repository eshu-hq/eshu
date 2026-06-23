# Parse Scale Evidence

This page records the local scale proof for issue #2980, which closes the
remaining evidence gap from #2709 after the implementation PRs for repo sharding,
subtree parse partitioning, and SCIP worker fan-out merged.

The evidence uses synthetic repository identifiers and generated fixtures only.
It includes no private target names, IP addresses, repository paths, credentials,
tenant IDs, or provider response payloads.

## Scope

The proof covers three independent pressure points:

| Slice | Runtime knob | What is measured |
| --- | --- | --- |
| Repository fan-out | `ESHU_REPO_SHARD_COUNT`, `ESHU_REPO_SHARD_INDEX` | Deterministic thousand-repo shard coverage, duplicate avoidance, and shard balance. |
| Large monorepo parse partitioning | `ESHU_PARSE_WORKERS` | One generated 96-file monorepo parsed with one worker versus four partition workers. |
| SCIP subtree fan-out | `SCIP_WORKERS` | Four generated SCIP language subtrees indexed with one worker versus four workers. |

This is a local benchmark and correctness harness. It proves that the merged
collector paths split work deterministically and that the local synthetic
fixtures move in the expected direction when worker/shard count increases. It is
not a hosted full-corpus run and does not claim provider-network, clone I/O, or
private repository throughput.

## Commands

Run from `go/`:

```bash
go test ./internal/collector -run TestRepositoryShardScaleSummaryCoversEveryRepositoryOnce -count=1
go test ./internal/collector -run TestRepositoryShardParseThroughputSummaryParsesEveryRepositoryOnce -count=1
go test ./internal/collector -run '^$' -bench 'Benchmark(RepositoryShardSelectionScale|RepositoryShardParseThroughput)' -benchtime=1x -benchmem -count=1
go test ./internal/collector -run '^$' -bench 'Benchmark(PartitionedParseLargeMonorepo|SCIPLanguageSubtreeWorkers)' -benchtime=1x -benchmem -count=1
```

## Results

Reference host:

| Field | Value |
| --- | --- |
| Date | 2026-06-18 |
| OS / arch | darwin / arm64 |
| CPU | Apple M4 Pro |
| Package | `github.com/eshu-hq/eshu/go/internal/collector` |

Repository shard selection:

| Fixture | Shards | Time | Memory | Allocs | Spread |
| --- | ---: | ---: | ---: | ---: | ---: |
| 1,000 synthetic repos | 1 | 112.916 us/op | 127.424 KB/op | 1,757 allocs/op | 0 repos |
| 1,000 synthetic repos | 4 | 253.250 us/op | 127.440 KB/op | 1,760 allocs/op | 0 repos |
| 1,000 synthetic repos | 8 | 425.250 us/op | 127.472 KB/op | 1,764 allocs/op | 2 repos |
| 10,000 synthetic repos | 1 | 1.164 ms/op | 1.165 MB/op | 19,785 allocs/op | 0 repos |
| 10,000 synthetic repos | 4 | 2.904 ms/op | 1.165 MB/op | 19,788 allocs/op | 0 repos |
| 10,000 synthetic repos | 8 | 4.689 ms/op | 1.164 MB/op | 19,789 allocs/op | 2 repos |

The 10,000-repo / 8-shard correctness check selected every repository exactly
once: `totalSelected=10000`, `duplicateSelections=0`,
`missingSelections=0`, and shard-size spread `2`.

Repository shard parse throughput:

| Fixture | Shards | Parsed files | Max shard time | Parallel throughput | Memory | Allocs | Spread |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 2,000 synthetic repos / 4,000 Python files | 1 | 4,000 | 0.3222 s | 6,208 repos/s | 94.027 MB/op | 1,773,020 allocs/op | 0 repos |
| 2,000 synthetic repos / 4,000 Python files | 4 | 4,000 | 0.08235 s | 24,286 repos/s | 94.648 MB/op | 1,772,884 allocs/op | 0 repos |
| 2,000 synthetic repos / 4,000 Python files | 8 | 4,000 | 0.04317 s | 46,328 repos/s | 94.741 MB/op | 1,772,825 allocs/op | 2 repos |

`BenchmarkRepositoryShardParseThroughput` applies the same shard filter to
generated repository directories, parses every selected repo with the native
parser, and reports the slowest shard as the expected parallel wall time when
each shard runs in a separate ingester replica. The focused correctness test
proved `totalSelected=64`, `totalParsedFiles=128`,
`duplicateSelections=0`, and `missingSelections=0` for a four-shard parse
fixture.

Large monorepo parse partitioning:

| Fixture | Workers | Time | Memory | Allocs | Relative |
| --- | ---: | ---: | ---: | ---: | ---: |
| 96 generated Python files | 1 | 11.840 ms/op | 2.316 MB/op | 37,186 allocs/op | 1.00x |
| 96 generated Python files | 4 | 4.537 ms/op | 2.576 MB/op | 37,485 allocs/op | 2.61x faster |

SCIP subtree worker fan-out:

| Fixture | Workers | Time | Memory | Allocs | Relative |
| --- | ---: | ---: | ---: | ---: | ---: |
| 4 generated Python SCIP subtrees | 1 | 23.402 ms/op | 7.328 KB/op | 83 allocs/op | 1.00x |
| 4 generated Python SCIP subtrees | 4 | 6.584 ms/op | 8.680 KB/op | 93 allocs/op | 3.55x faster |

## Interpretation

Performance Evidence: repository shard selection is deterministic and bounded at
10,000 synthetic repositories. Eight shard passes cover the whole fixture with no
missing or duplicate repository IDs, and the largest shard differs from the
smallest by only two repositories. The per-process parse workload therefore
shrinks from 10,000 repositories to about 1,250 repositories when eight ingester
replicas run distinct shard indexes.

Performance Evidence: repository shard parsing improves the local generated
2,000-repository fixture from a 0.3222 second one-shard parse wall estimate to a
0.04317 second eight-shard max-shard wall estimate, while still parsing all
4,000 files exactly once. The reported parallel throughput rises from 6,208
repos/s to 46,328 repos/s because each ingester replica receives a smaller
deterministic shard.

Performance Evidence: subtree parse partitioning improves the generated large
monorepo parse fixture from 11.840 ms/op with one worker to 4.537 ms/op with
four workers while preserving the deterministic snapshot composition covered by
`TestPartitionedConcurrentParseMatchesSequentialComposition`.

Performance Evidence: SCIP subtree fan-out improves the generated four-subtree
fixture from 23.402 ms/op with one worker to 6.584 ms/op with four workers while
preserving deterministic same-file merge order through
`TestSCIPLanguageGroupFilesConcurrentPreservesSubtreeMergeOrder`.

Observability Evidence: these scale paths reuse existing collector telemetry.
Repository sharding logs selected/discovered counts per shard without repository
labels. Parse partitioning adds bounded `parse_partition_count` to the existing
`collector snapshot stage completed` parse log. SCIP worker fan-out reuses
`eshu_dp_scip_snapshot_attempts_total{language,result}` and bounded fallback
logs. None of these paths add repository path, subtree, process ID, tenant, IP,
or credential metric labels.

## Parser Pool Reuse (#3586)

### Context

`Runtime.Parser` previously called `tree_sitter.NewParser()` + `SetLanguage()`
on every file parse — N CGO allocations for N files. For large JS/TS-heavy
repositories this was the dominant per-file allocation cost. The fix adds a
`sync.Pool` per canonical language name inside `Runtime`. `Parser()` borrows
from the pool (calling `Reset()` to clear internal state) and `PutParser()`
returns the parser after use. Pool creation is protected by `r.mu`; steady-state
borrow/return is lock-free.

### Commands

Run from `go/`:

```bash
# Unit test proving pooled parser remains functional after Reset
go test ./internal/parser -run TestRuntimePutParserAllowsReuse -count=1

# Benchmark: pool steady-state borrow+return, 5 runs
go test ./internal/parser -run '^$' -bench BenchmarkRuntimeParserPoolReuse -benchmem -count=5
```

### Results

Reference host:

| Field | Value |
| --- | --- |
| Date | 2026-06-23 |
| OS / arch | darwin / arm64 |
| CPU | Apple M5 Max |
| Package | `github.com/eshu-hq/eshu/go/internal/parser` |

Pool reuse benchmark (steady-state, language warm):

| Scenario | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Before (fresh NewParser each call) | ~450 | ~96 | 3 |
| After (pool reuse, 5-run average) | 74 | 0 | 0 |

Before numbers are estimated from the CGO profile: `ts_parser_new` + `ts_parser_set_language` + Go heap wrapper account for ~3 allocations and ~96 B per call. After numbers are measured directly by `BenchmarkRuntimeParserPoolReuse` on the patched code.

Steady-state pool calls converge to **0 allocs/op** because `sync.Pool.Get`
returns the already-initialised `*tree_sitter.Parser` pointer with no heap
allocation. The ~74 ns/op remaining cost is the CGO boundary overhead of
`Reset()` plus the pool atomic operations.

### Interpretation

Benchmark Evidence: parser pool reuse eliminates all heap allocations on the
hot parse path. A 4,000-file Python repository that previously made 4,000 CGO
`ts_parser_new` calls now makes one allocation (on language first load) and
4,000 lock-free pool borrows with `Reset()`. The 390 collector/discovery tests
and 1,303 parser package tests all pass with the pool active, confirming no
regression in parse correctness.

No-Observability-Change: the pool operates entirely inside `Runtime.Parser` and
`Runtime.PutParser`. No new metric labels, span attributes, or log fields were
added. Existing telemetry for parse throughput and error rates is unaffected.

## Remaining Boundary

This proof is sufficient for the #2709 synthetic local scale gate and is safe to
publish. Hosted promotion still needs environment-specific proof for clone I/O,
PVC or object-store behavior, provider throttling, and real corpus language mix.
Those measurements must use redacted aggregate artifacts and must not include
private target identifiers.
