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
go test ./internal/collector -run '^$' -bench BenchmarkRepositoryShardSelectionScale -benchtime=1x -benchmem -count=1
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
| 1,000 synthetic repos | 1 | 173.833 us/op | 127.424 KB/op | 1,757 allocs/op | 0 repos |
| 1,000 synthetic repos | 4 | 418.292 us/op | 127.440 KB/op | 1,760 allocs/op | 0 repos |
| 1,000 synthetic repos | 8 | 599.459 us/op | 127.192 KB/op | 1,761 allocs/op | 2 repos |
| 10,000 synthetic repos | 1 | 1.306 ms/op | 1.165 MB/op | 19,785 allocs/op | 0 repos |
| 10,000 synthetic repos | 4 | 3.754 ms/op | 1.165 MB/op | 19,788 allocs/op | 0 repos |
| 10,000 synthetic repos | 8 | 5.325 ms/op | 1.165 MB/op | 19,792 allocs/op | 2 repos |

The 10,000-repo / 8-shard correctness check selected every repository exactly
once: `totalSelected=10000`, `duplicateSelections=0`,
`missingSelections=0`, and shard-size spread `2`.

Large monorepo parse partitioning:

| Fixture | Workers | Time | Memory | Allocs | Relative |
| --- | ---: | ---: | ---: | ---: | ---: |
| 96 generated Python files | 1 | 14.109 ms/op | 2.321 MB/op | 37,190 allocs/op | 1.00x |
| 96 generated Python files | 4 | 4.988 ms/op | 2.510 MB/op | 37,489 allocs/op | 2.83x faster |

SCIP subtree worker fan-out:

| Fixture | Workers | Time | Memory | Allocs | Relative |
| --- | ---: | ---: | ---: | ---: | ---: |
| 4 generated Python SCIP subtrees | 1 | 23.248 ms/op | 7.360 KB/op | 84 allocs/op | 1.00x |
| 4 generated Python SCIP subtrees | 4 | 5.488 ms/op | 11.464 KB/op | 100 allocs/op | 4.24x faster |

## Interpretation

Performance Evidence: repository shard selection is deterministic and bounded at
10,000 synthetic repositories. Eight shard passes cover the whole fixture with no
missing or duplicate repository IDs, and the largest shard differs from the
smallest by only two repositories. The per-process parse workload therefore
shrinks from 10,000 repositories to about 1,250 repositories when eight ingester
replicas run distinct shard indexes.

Performance Evidence: subtree parse partitioning improves the generated large
monorepo parse fixture from 14.109 ms/op with one worker to 4.988 ms/op with
four workers while preserving the deterministic snapshot composition covered by
`TestPartitionedConcurrentParseMatchesSequentialComposition`.

Performance Evidence: SCIP subtree fan-out improves the generated four-subtree
fixture from 23.248 ms/op with one worker to 5.488 ms/op with four workers while
preserving deterministic same-file merge order through
`TestSCIPLanguageGroupFilesConcurrentPreservesSubtreeMergeOrder`.

Observability Evidence: these scale paths reuse existing collector telemetry.
Repository sharding logs selected/discovered counts per shard without repository
labels. Parse partitioning adds bounded `parse_partition_count` to the existing
`collector snapshot stage completed` parse log. SCIP worker fan-out reuses
`eshu_dp_scip_snapshot_attempts_total{language,result}` and bounded fallback
logs. None of these paths add repository path, subtree, process ID, tenant, IP,
or credential metric labels.

## Remaining Boundary

This proof is sufficient for the #2709 synthetic local scale gate and is safe to
publish. Hosted promotion still needs environment-specific proof for clone I/O,
PVC or object-store behavior, provider throttling, and real corpus language mix.
Those measurements must use redacted aggregate artifacts and must not include
private target identifiers.
